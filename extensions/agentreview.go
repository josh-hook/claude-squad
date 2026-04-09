package extensions

import (
	"claude-squad/log"
	"claude-squad/session"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"claude-squad/config"
)

// AgentReviewExtension prompts idle instances to run a code review subagent
// on their PR when new commits are pushed.
type AgentReviewExtension struct {
	mu    sync.Mutex
	state reviewState
}

// reviewState tracks the last reviewed commit per instance.
type reviewState struct {
	// Maps instance title -> last reviewed commit SHA.
	LastReviewed map[string]string `json:"last_reviewed"`
}

// NewAgentReviewExtension creates a new agent review extension, loading persisted state.
func NewAgentReviewExtension() *AgentReviewExtension {
	e := &AgentReviewExtension{
		state: reviewState{LastReviewed: make(map[string]string)},
	}
	e.loadState()
	return e
}

func (e *AgentReviewExtension) Name() string { return "agent-review" }

func (e *AgentReviewExtension) Check(inst *session.Instance) bool {
	wt, err := inst.GetGitWorktree()
	if err != nil || wt == nil {
		return false
	}

	repoPath := wt.GetRepoPath()
	branch := wt.GetBranchName()
	if repoPath == "" || branch == "" {
		return false
	}

	log.InfoLog.Printf("[agent-review] %q: checking branch %s", inst.Title, branch)

	// Check if a non-draft PR exists.
	pr, err := getPRInfo(repoPath, branch)
	if err != nil {
		log.InfoLog.Printf("[agent-review] %q: no PR found: %v", inst.Title, err)
		return false
	}
	if pr == nil {
		log.InfoLog.Printf("[agent-review] %q: no PR for branch %s", inst.Title, branch)
		return false
	}
	if pr.isDraft {
		log.InfoLog.Printf("[agent-review] %q: PR #%d is draft, skipping", inst.Title, pr.number)
		return false
	}

	// Get the latest commit on the PR.
	headSHA, err := getPRHeadSHA(repoPath, branch)
	if err != nil || headSHA == "" {
		log.InfoLog.Printf("[agent-review] %q: could not get head SHA: %v", inst.Title, err)
		return false
	}

	// Check if we already reviewed this commit.
	e.mu.Lock()
	lastReviewed := e.state.LastReviewed[inst.Title]
	e.mu.Unlock()

	if lastReviewed == headSHA {
		log.InfoLog.Printf("[agent-review] %q: already reviewed commit %s, skipping", inst.Title, headSHA[:8])
		return false
	}

	log.InfoLog.Printf("[agent-review] %q: new commit %s (last reviewed: %s), prompting review",
		inst.Title, headSHA[:8], truncSHA(lastReviewed))

	prompt := "Run the pr-reviewer subagent on this PR, act on any of its feedback."
	if err := inst.SendPrompt(prompt); err != nil {
		log.ErrorLog.Printf("[agent-review] %q: failed to send prompt: %v", inst.Title, err)
		return false
	}

	// Record that we reviewed this commit.
	e.mu.Lock()
	e.state.LastReviewed[inst.Title] = headSHA
	e.mu.Unlock()
	e.saveState()

	return true
}

// getPRHeadSHA returns the head commit SHA for the PR on the given branch.
func getPRHeadSHA(repoDir, branch string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "headRefOid", "-q", ".headRefOid")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v (output=%s)", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func truncSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "none"
	}
	return sha
}

// State file persistence

func stateFilePath() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "agent_review_state.json")
}

func (e *AgentReviewExtension) loadState() {
	path := stateFilePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist yet, that's fine
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := json.Unmarshal(data, &e.state); err != nil {
		log.WarningLog.Printf("[agent-review] failed to parse state file: %v", err)
		e.state.LastReviewed = make(map[string]string)
	}
}

func (e *AgentReviewExtension) saveState() {
	path := stateFilePath()
	if path == "" {
		return
	}
	e.mu.Lock()
	data, err := json.MarshalIndent(e.state, "", "  ")
	e.mu.Unlock()
	if err != nil {
		log.ErrorLog.Printf("[agent-review] failed to marshal state: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.ErrorLog.Printf("[agent-review] failed to write state file: %v", err)
	}
}
