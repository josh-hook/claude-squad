package git

import (
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Setup creates a new worktree for the session
func (g *GitWorktree) Setup() error {
	// Ensure worktrees directory exists early (can be done in parallel with branch check)
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return err
	}

	// Fetch the default branch so diffs are computed against the latest main/master.
	defaultBranch := FindDefaultBranch(g.repoPath)
	fetchCmd := exec.Command("git", "-C", g.repoPath, "fetch", "origin", defaultBranch)
	if err := fetchCmd.Run(); err != nil {
		log.WarningLog.Printf("failed to fetch %s: %v (continuing anyway)", defaultBranch, err)
	}

	// If this worktree uses a pre-existing branch, always set up from that branch
	// (it may exist locally or only on the remote).
	if g.isExistingBranch {
		if err := g.setupFromExistingBranch(); err != nil {
			return err
		}
	} else {
		// Check if branch exists using git CLI (much faster than go-git PlainOpen)
		_, err = g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
		if err == nil {
			if err := g.setupFromExistingBranch(); err != nil {
				return err
			}
		} else {
			if err := g.setupNewWorktree(); err != nil {
				return err
			}
		}
	}

	// Set baseCommitSHA to the merge-base between the worktree branch and origin/main.
	// This gives a PR-style diff: only the changes on the branch, not changes on main
	// since the branch was created.
	if g.baseCommitSHA == "" {
		originRef := fmt.Sprintf("origin/%s", defaultBranch)
		if sha, err := g.runGitCommand(g.worktreePath, "merge-base", originRef, "HEAD"); err == nil {
			g.baseCommitSHA = strings.TrimSpace(sha)
		} else if sha, err := g.runGitCommand(g.repoPath, "rev-parse", originRef); err == nil {
			// Fallback: use origin/main directly if merge-base fails
			g.baseCommitSHA = strings.TrimSpace(sha)
		}
	}

	// Create .claude/settings.local.json so Claude Code trusts this worktree
	if err := g.writeClaudeSettings(); err != nil {
		log.WarningLog.Printf("failed to write claude settings to worktree: %v", err)
	}

	return nil
}

// setupFromExistingBranch creates a worktree from an existing branch.
// If the worktree already exists on disk, it is reused.
func (g *GitWorktree) setupFromExistingBranch() error {
	// If the worktree directory already exists, reuse it.
	if info, err := os.Stat(g.worktreePath); err == nil && info.IsDir() {
		log.InfoLog.Printf("worktree already exists at %s, reusing", g.worktreePath)
		return nil
	}

	// Prune stale worktree references and remove any existing worktree using this branch.
	// This handles the case where a previous session created a worktree at a different
	// path (e.g. with a different timestamp suffix) that still holds the branch.
	_, _ = g.runGitCommand(g.repoPath, "worktree", "prune")
	g.removeWorktreeForBranch()

	// Check if the local branch exists
	_, localErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
	if localErr != nil {
		// Local branch doesn't exist — check if remote tracking branch exists
		_, remoteErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/remotes/origin/%s", g.branchName))
		if remoteErr != nil {
			return fmt.Errorf("branch %s not found locally or on remote", g.branchName)
		}
		// Create a local tracking branch via worktree add -b
		if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, fmt.Sprintf("origin/%s", g.branchName)); err != nil {
			return fmt.Errorf("failed to create worktree from remote branch %s: %w", g.branchName, err)
		}
		return nil
	}

	// Create a new worktree from the existing local branch
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", g.worktreePath, g.branchName); err != nil {
		return fmt.Errorf("failed to create worktree from branch %s: %w", g.branchName, err)
	}

	return nil
}

// removeWorktreeForBranch finds and force-removes any existing worktree that has this branch checked out.
func (g *GitWorktree) removeWorktreeForBranch() {
	output, err := g.runGitCommand(g.repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return
	}

	// Parse porcelain output to find the worktree using our branch.
	// Format: "worktree <path>\nHEAD <sha>\nbranch refs/heads/<name>\n\n"
	branchRef := fmt.Sprintf("refs/heads/%s", g.branchName)
	var currentPath string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			if ref == branchRef && currentPath != "" && currentPath != g.worktreePath {
				log.InfoLog.Printf("removing old worktree at %s that holds branch %s", currentPath, g.branchName)
				_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", currentPath)
			}
		}
	}
}

// setupNewWorktree creates a new worktree from HEAD.
// If the worktree already exists on disk, it is reused.
func (g *GitWorktree) setupNewWorktree() error {
	// If the worktree directory already exists, reuse it.
	if info, err := os.Stat(g.worktreePath); err == nil && info.IsDir() {
		log.InfoLog.Printf("worktree already exists at %s, reusing", g.worktreePath)
		return nil
	}

	// Clean up any stale worktree reference
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath)

	// Clean up any existing branch using git CLI (much faster than go-git PlainOpen)
	_, _ = g.runGitCommand(g.repoPath, "branch", "-D", g.branchName)

	output, err := g.runGitCommand(g.repoPath, "rev-parse", "HEAD")
	if err != nil {
		if strings.Contains(err.Error(), "fatal: ambiguous argument 'HEAD'") ||
			strings.Contains(err.Error(), "fatal: not a valid object name") ||
			strings.Contains(err.Error(), "fatal: HEAD: not a valid object name") {
			return fmt.Errorf("this appears to be a brand new repository: please create an initial commit before creating an instance")
		}
		return fmt.Errorf("failed to get HEAD commit hash: %w", err)
	}
	headCommit := strings.TrimSpace(string(output))

	// Create a new worktree from the HEAD commit
	// Otherwise, we'll inherit uncommitted changes from the previous worktree.
	// This way, we can start the worktree with a clean slate.
	// TODO: we might want to give an option to use main/master instead of the current branch.
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, headCommit); err != nil {
		return fmt.Errorf("failed to create worktree from commit %s: %w", headCommit, err)
	}

	return nil
}

// writeClaudeSettings creates a .claude/settings.local.json in the worktree
// so that Claude Code trusts the directory and allows edits without prompting.
func (g *GitWorktree) writeClaudeSettings() error {
	claudeDir := filepath.Join(g.worktreePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	settings := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": []string{
				"Edit",
				"Write",
				"Bash(git add:*)",
				"Bash(git commit:*)",
				"Bash(git diff:*)",
				"Bash(git log:*)",
				"Bash(git status:*)",
				"Bash(git push:*)",
				"Bash(git checkout:*)",
				"Bash(git branch:*)",
			},
			"deny": []string{},
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal claude settings: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write claude settings: %w", err)
	}

	return nil
}

// Cleanup removes the worktree and associated branch
func (g *GitWorktree) Cleanup() error {
	var errs []error

	// Check if worktree path exists before attempting removal
	if _, err := os.Stat(g.worktreePath); err == nil {
		// Remove the worktree using git command
		if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
			errs = append(errs, err)
		}
	} else if !os.IsNotExist(err) {
		// Only append error if it's not a "not exists" error
		errs = append(errs, fmt.Errorf("failed to check worktree path: %w", err))
	}

	// Delete the branch using git CLI, but skip if this is a pre-existing branch
	if !g.isExistingBranch {
		if _, err := g.runGitCommand(g.repoPath, "branch", "-D", g.branchName); err != nil {
			// Only log if it's not a "branch not found" error
			if !strings.Contains(err.Error(), "not found") {
				errs = append(errs, fmt.Errorf("failed to remove branch %s: %w", g.branchName, err))
			}
		}
	}

	// Prune the worktree to clean up any remaining references
	if err := g.Prune(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return g.combineErrors(errs)
	}

	return nil
}

// Remove removes the worktree but keeps the branch
func (g *GitWorktree) Remove() error {
	// Remove the worktree using git command
	if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// Prune removes all working tree administrative files and directories
func (g *GitWorktree) Prune() error {
	if _, err := g.runGitCommand(g.repoPath, "worktree", "prune"); err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}
	return nil
}

// CleanupWorktrees removes all worktrees and their associated branches
func CleanupWorktrees() error {
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return fmt.Errorf("failed to read worktree directory: %w", err)
	}

	// Get a list of all branches associated with worktrees
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	// Parse the output to extract branch names
	worktreeBranches := make(map[string]string)
	currentWorktree := ""
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			branchPath := strings.TrimPrefix(line, "branch ")
			// Extract branch name from refs/heads/branch-name
			branchName := strings.TrimPrefix(branchPath, "refs/heads/")
			if currentWorktree != "" {
				worktreeBranches[currentWorktree] = branchName
			}
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			worktreePath := filepath.Join(worktreesDir, entry.Name())

			// Delete the branch associated with this worktree if found
			for path, branch := range worktreeBranches {
				if strings.Contains(path, entry.Name()) {
					// Delete the branch
					deleteCmd := exec.Command("git", "branch", "-D", branch)
					if err := deleteCmd.Run(); err != nil {
						// Log the error but continue with other worktrees
						log.ErrorLog.Printf("failed to delete branch %s: %v", branch, err)
					}
					break
				}
			}

			// Remove the worktree directory
			os.RemoveAll(worktreePath)
		}
	}

	// You have to prune the cleaned up worktrees.
	cmd = exec.Command("git", "worktree", "prune")
	_, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}

	return nil
}
