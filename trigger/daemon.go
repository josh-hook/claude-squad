package trigger

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	triggerStateFileName = "trigger_state.json"
	defaultPollInterval  = 60 // seconds
)

// triggerState persists which issues have already been processed so we don't
// create duplicate sessions across daemon restarts.
type triggerState struct {
	// ProcessedIssues maps Linear issue ID → the time it was processed.
	ProcessedIssues map[string]time.Time `json:"processed_issues"`
}

// RunTriggerDaemon is the main entry point for the trigger daemon. It polls
// Linear on a configurable interval, checks issue readiness, and creates
// claude code sessions for issues that are ready.
func RunTriggerDaemon(cfg *config.Config) error {
	tc := cfg.Trigger
	if tc == nil {
		return fmt.Errorf("trigger config is not set — add a \"trigger\" section to config.json")
	}

	apiKey := os.Getenv("LINEAR_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LINEAR_API_KEY environment variable is required")
	}

	if tc.AssigneeEmail == "" {
		return fmt.Errorf("trigger.assignee_email is required in config")
	}
	if tc.RepoPath == "" {
		return fmt.Errorf("trigger.repo_path is required in config")
	}

	// Resolve ~ in repo path.
	repoPath := expandHome(tc.RepoPath)

	program := cfg.GetProgram()
	client := NewLinearClient(apiKey)

	processed, err := loadTriggerState()
	if err != nil {
		log.WarningLog.Printf("could not load trigger state, starting fresh: %v", err)
		processed = &triggerState{ProcessedIssues: make(map[string]time.Time)}
	}

	pollInterval := defaultPollInterval
	if tc.PollInterval > 0 {
		pollInterval = tc.PollInterval
	}

	log.InfoLog.Printf("trigger daemon starting: email=%s repo=%s poll=%ds",
		tc.AssigneeEmail, repoPath, pollInterval)

	ticker := time.NewTicker(time.Duration(pollInterval) * time.Second)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	process := func() {
		issues, err := client.FetchAssignedIssues(tc.AssigneeEmail)
		if err != nil {
			log.ErrorLog.Printf("failed to fetch issues: %v", err)
			return
		}
		log.InfoLog.Printf("fetched %d open issues assigned to %s", len(issues), tc.AssigneeEmail)

		for _, issue := range issues {
			if _, done := processed.ProcessedIssues[issue.ID]; done {
				continue
			}

			if !issue.IsReady() {
				log.InfoLog.Printf("issue %s (%s) has unresolved dependencies, skipping",
					issue.Identifier, issue.Title)
				continue
			}

			log.InfoLog.Printf("issue %s is ready, creating session: %s", issue.Identifier, issue.Title)

			if err := createSessionForIssue(issue, repoPath, program); err != nil {
				log.ErrorLog.Printf("failed to create session for %s: %v", issue.Identifier, err)
				continue
			}

			processed.ProcessedIssues[issue.ID] = time.Now()
			if err := saveTriggerState(processed); err != nil {
				log.ErrorLog.Printf("failed to persist trigger state: %v", err)
			}
		}
	}

	// Run immediately on startup, then on each tick.
	process()

	for {
		select {
		case <-ticker.C:
			process()
		case sig := <-sigCh:
			log.InfoLog.Printf("trigger daemon received %s, shutting down", sig)
			return nil
		}
	}
}

// createSessionForIssue creates a new claude code session for the given Linear
// issue. It sets up a worktree in the configured repo, starts a tmux session,
// and sends the issue details as the initial prompt.
func createSessionForIssue(issue Issue, repoPath, program string) error {
	title := sanitizeTitle(fmt.Sprintf("%s %s", issue.Identifier, issue.Title))

	instance, err := session.NewInstance(session.InstanceOptions{
		Title:   title,
		Path:    repoPath,
		Program: program,
	})
	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}

	prompt := formatPrompt(issue)
	instance.Prompt = prompt
	instance.OriginalPrompt = prompt

	if err := instance.Start(true); err != nil {
		return fmt.Errorf("start instance: %w", err)
	}

	// Send the prompt to the agent once it finishes initialising.
	go func() {
		if err := instance.SendPromptWhenReady(prompt); err != nil {
			log.ErrorLog.Printf("failed to send prompt for %s: %v", issue.Identifier, err)
		}
	}()

	// Persist the instance so the TUI can see it.
	if err := appendInstanceToStorage(instance); err != nil {
		log.ErrorLog.Printf("failed to save instance for %s: %v", issue.Identifier, err)
		// Non-fatal: the session is running, just not visible in the TUI yet.
	}

	log.InfoLog.Printf("session created for %s", issue.Identifier)
	return nil
}

// appendInstanceToStorage adds a single instance to the persisted state without
// loading (and therefore restarting) existing instances.
func appendInstanceToStorage(instance *session.Instance) error {
	state := config.LoadState()

	var existing []session.InstanceData
	raw := state.GetInstances()
	if err := json.Unmarshal(raw, &existing); err != nil {
		existing = []session.InstanceData{}
	}

	existing = append(existing, instance.ToInstanceData())

	newJSON, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal instances: %w", err)
	}

	return state.SaveInstances(newJSON)
}

// formatPrompt builds the initial prompt sent to the claude code agent.
func formatPrompt(issue Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You have been assigned the following Linear issue to implement.\n\n")
	fmt.Fprintf(&b, "Issue: %s\n", issue.Identifier)
	fmt.Fprintf(&b, "Title: %s\n", issue.Title)
	if issue.Description != "" {
		fmt.Fprintf(&b, "\nDescription:\n%s\n", issue.Description)
	}
	fmt.Fprintf(&b, "\nPlease implement this issue. When you are done, commit your changes and create a pull request.")
	return b.String()
}

// --- trigger state persistence ---

func triggerStatePath() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, triggerStateFileName), nil
}

func loadTriggerState() (*triggerState, error) {
	p, err := triggerStatePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &triggerState{ProcessedIssues: make(map[string]time.Time)}, nil
		}
		return nil, err
	}

	var ts triggerState
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, err
	}
	if ts.ProcessedIssues == nil {
		ts.ProcessedIssues = make(map[string]time.Time)
	}
	return &ts, nil
}

func saveTriggerState(ts *triggerState) error {
	p, err := triggerStatePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// --- helpers ---

var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return strings.Trim(s, "-")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
