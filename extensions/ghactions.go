package extensions

import (
	"claude-squad/log"
	"claude-squad/session"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GHActionsExtension monitors GitHub PRs for failing checks and unresolved
// review comments, then prompts claude to fix them.
type GHActionsExtension struct {
	// lastPrompted caches the prompt text per instance title to avoid
	// re-prompting for the same failures.
	lastPrompted map[string]string
}

// NewGHActionsExtension creates a new GitHub Actions extension.
func NewGHActionsExtension() *GHActionsExtension {
	return &GHActionsExtension{
		lastPrompted: make(map[string]string),
	}
}

func (e *GHActionsExtension) Name() string { return "github-actions" }

// Check inspects the instance's PR for failing checks or unresolved comments.
func (e *GHActionsExtension) Check(inst *session.Instance) bool {
	wt, err := inst.GetGitWorktree()
	if err != nil || wt == nil {
		return false
	}

	repoPath := wt.GetRepoPath()
	branch := wt.GetBranchName()
	if repoPath == "" || branch == "" {
		return false
	}

	// Check if a non-draft PR exists for this branch.
	pr, err := getPRInfo(repoPath, branch)
	if err != nil || pr == nil || pr.isDraft {
		return false
	}

	// Build a prompt from failing checks and unresolved comments.
	prompt := e.buildPrompt(repoPath, branch, pr.number)
	if prompt == "" {
		return false
	}

	// Deduplicate: skip if we already prompted with the exact same text.
	if e.lastPrompted[inst.Title] == prompt {
		return false
	}

	if err := inst.SendPrompt(prompt); err != nil {
		log.ErrorLog.Printf("[github-actions] failed to prompt %q: %v", inst.Title, err)
		return false
	}
	e.lastPrompted[inst.Title] = prompt
	return true
}

func (e *GHActionsExtension) buildPrompt(repoDir, branch string, prNumber int) string {
	var parts []string

	if failures := getFailingChecks(repoDir, branch); failures != "" {
		parts = append(parts, "The following CI checks are failing on this PR:\n\n"+failures)
	}

	if comments := getUnresolvedComments(repoDir, prNumber); comments != "" {
		parts = append(parts, "The following review comments are unresolved on this PR:\n\n"+comments)
	}

	if len(parts) == 0 {
		return ""
	}

	return "There are issues on the GitHub PR that need to be addressed:\n\n" +
		strings.Join(parts, "\n\n---\n\n") +
		"\n\nPlease fix these issues."
}

// prInfo holds basic PR metadata.
type prInfo struct {
	number  int
	isDraft bool
}

// getPRInfo fetches PR info for the given branch. Returns nil if no PR exists.
func getPRInfo(repoDir, branch string) (*prInfo, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number,isDraft")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result struct {
		Number  int  `json:"number"`
		IsDraft bool `json:"isDraft"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return &prInfo{number: result.Number, isDraft: result.IsDraft}, nil
}

// getFailingChecks returns a summary of failing CI checks, or empty string if all pass.
func getFailingChecks(repoDir, branch string) string {
	cmd := exec.Command("gh", "pr", "checks", branch,
		"--json", "name,conclusion,detailsUrl",
		"-q", `.[] | select(.conclusion == "failure" or .conclusion == "startup_failure") | (.name + ": " + .detailsUrl)`)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return ""
	}

	// Try to get failed run logs for more context.
	logOutput := getFailedRunLogs(repoDir, branch)
	if logOutput != "" {
		result += "\n\nFailed run logs (truncated):\n" + logOutput
	}

	return result
}

// getFailedRunLogs fetches the most recent failed run's logs for the branch.
func getFailedRunLogs(repoDir, branch string) string {
	// Get the most recent failed run ID
	cmd := exec.Command("gh", "run", "list", "--branch", branch,
		"--status", "failure", "--limit", "1", "--json", "databaseId",
		"-q", ".[0].databaseId")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	runID := strings.TrimSpace(string(out))
	if runID == "" {
		return ""
	}

	// Fetch the failed logs
	logCmd := exec.Command("gh", "run", "view", runID, "--log-failed")
	logCmd.Dir = repoDir
	logOut, err := logCmd.Output()
	if err != nil {
		return ""
	}

	result := strings.TrimSpace(string(logOut))
	// Truncate to avoid overwhelming the tmux input
	const maxLogLen = 3000
	if len(result) > maxLogLen {
		result = result[len(result)-maxLogLen:] + "\n...(truncated to last 3000 chars)"
	}
	return result
}

// getUnresolvedComments returns a summary of unresolved PR review comments.
func getUnresolvedComments(repoDir string, prNumber int) string {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", prNumber),
		"--json", "reviewThreads",
		"-q", `.reviewThreads[] | select(.isResolved == false) | .comments[0] | (.path + ":" + (.line|tostring) + " - " + .body)`)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	result := strings.TrimSpace(string(out))
	// Truncate if too long
	const maxLen = 3000
	if len(result) > maxLen {
		result = result[:maxLen] + "\n...(truncated)"
	}
	return result
}
