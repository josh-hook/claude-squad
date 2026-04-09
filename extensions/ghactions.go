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
		log.InfoLog.Printf("[github-actions] %q: no worktree (err=%v)", inst.Title, err)
		return false
	}

	repoPath := wt.GetRepoPath()
	branch := wt.GetBranchName()
	if repoPath == "" || branch == "" {
		log.InfoLog.Printf("[github-actions] %q: empty repo=%q or branch=%q", inst.Title, repoPath, branch)
		return false
	}

	log.InfoLog.Printf("[github-actions] %q: checking PR for branch %s in %s", inst.Title, branch, repoPath)

	// Check if a non-draft PR exists for this branch.
	pr, err := getPRInfo(repoPath, branch)
	if err != nil {
		log.InfoLog.Printf("[github-actions] %q: no PR found: %v", inst.Title, err)
		return false
	}
	if pr == nil {
		log.InfoLog.Printf("[github-actions] %q: no PR exists for branch %s", inst.Title, branch)
		return false
	}
	if pr.isDraft {
		log.InfoLog.Printf("[github-actions] %q: PR #%d is draft, skipping", inst.Title, pr.number)
		return false
	}

	log.InfoLog.Printf("[github-actions] %q: found PR #%d, checking for issues", inst.Title, pr.number)

	// Build a prompt from failing checks and unresolved comments.
	prompt := e.buildPrompt(repoPath, branch, pr.number)
	if prompt == "" {
		log.InfoLog.Printf("[github-actions] %q: PR #%d has no issues", inst.Title, pr.number)
		return false
	}

	// Deduplicate: skip if we already prompted with the exact same text.
	if e.lastPrompted[inst.Title] == prompt {
		log.InfoLog.Printf("[github-actions] %q: same issues as last check, skipping", inst.Title)
		return false
	}

	log.InfoLog.Printf("[github-actions] %q: prompting claude with PR issues (len=%d)", inst.Title, len(prompt))
	if err := inst.SendPrompt(prompt); err != nil {
		log.ErrorLog.Printf("[github-actions] %q: failed to send prompt: %v", inst.Title, err)
		return false
	}
	e.lastPrompted[inst.Title] = prompt
	return true
}

func (e *GHActionsExtension) buildPrompt(repoDir, branch string, prNumber int) string {
	var parts []string

	failures := getFailingChecks(repoDir, branch)
	if failures != "" {
		log.InfoLog.Printf("[github-actions] found failing checks for branch %s", branch)
		parts = append(parts, "The following CI checks are failing on this PR:\n\n"+failures)
	}

	comments := getUnresolvedComments(repoDir, prNumber)
	if comments != "" {
		log.InfoLog.Printf("[github-actions] found unresolved comments on PR #%d", prNumber)
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
	log.InfoLog.Printf("[github-actions] running: gh pr view %s --json number,isDraft (dir=%s)", branch, repoDir)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result struct {
		Number  int  `json:"number"`
		IsDraft bool `json:"isDraft"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		log.ErrorLog.Printf("[github-actions] failed to parse PR info: %v (output=%q)", err, string(out))
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
	log.InfoLog.Printf("[github-actions] running: gh pr checks %s (dir=%s)", branch, repoDir)
	out, err := cmd.Output()
	if err != nil {
		log.InfoLog.Printf("[github-actions] gh pr checks failed: %v", err)
		return ""
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		log.InfoLog.Printf("[github-actions] no failing checks for %s", branch)
		return ""
	}

	log.InfoLog.Printf("[github-actions] failing checks:\n%s", result)

	// Try to get failed run logs for more context.
	logOutput := getFailedRunLogs(repoDir, branch)
	if logOutput != "" {
		log.InfoLog.Printf("[github-actions] got %d chars of failed run logs", len(logOutput))
		result += "\n\nFailed run logs (truncated):\n" + logOutput
	}

	return result
}

// getFailedRunLogs fetches the most recent failed run's logs for the branch.
func getFailedRunLogs(repoDir, branch string) string {
	cmd := exec.Command("gh", "run", "list", "--branch", branch,
		"--status", "failure", "--limit", "1", "--json", "databaseId",
		"-q", ".[0].databaseId")
	cmd.Dir = repoDir
	log.InfoLog.Printf("[github-actions] running: gh run list --branch %s --status failure", branch)
	out, err := cmd.Output()
	if err != nil {
		log.InfoLog.Printf("[github-actions] gh run list failed: %v", err)
		return ""
	}
	runID := strings.TrimSpace(string(out))
	if runID == "" {
		log.InfoLog.Printf("[github-actions] no failed runs found for %s", branch)
		return ""
	}

	log.InfoLog.Printf("[github-actions] fetching logs for failed run %s", runID)
	logCmd := exec.Command("gh", "run", "view", runID, "--log-failed")
	logCmd.Dir = repoDir
	logOut, err := logCmd.Output()
	if err != nil {
		log.InfoLog.Printf("[github-actions] gh run view --log-failed failed: %v", err)
		return ""
	}

	result := strings.TrimSpace(string(logOut))
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
	log.InfoLog.Printf("[github-actions] running: gh pr view %d --json reviewThreads (dir=%s)", prNumber, repoDir)
	out, err := cmd.Output()
	if err != nil {
		log.InfoLog.Printf("[github-actions] gh pr view reviewThreads failed: %v", err)
		return ""
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		log.InfoLog.Printf("[github-actions] no unresolved comments on PR #%d", prNumber)
	} else {
		log.InfoLog.Printf("[github-actions] found unresolved comments on PR #%d (%d chars)", prNumber, len(result))
	}
	const maxLen = 3000
	if len(result) > maxLen {
		result = result[:maxLen] + "\n...(truncated)"
	}
	return result
}
