package git

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// sanitizeBranchName transforms an arbitrary string into a Git branch name friendly string.
// Note: Git branch names have several rules, so this function uses a simple approach
// by allowing only a safe subset of characters.
func sanitizeBranchName(s string) string {
	// Convert to lower-case
	s = strings.ToLower(s)

	// Replace spaces with a dash
	s = strings.ReplaceAll(s, " ", "-")

	// Remove any characters not allowed in our safe subset.
	// Here we allow: letters, digits, dash, underscore, slash, and dot.
	re := regexp.MustCompile(`[^a-z0-9\-_/.]+`)
	s = re.ReplaceAllString(s, "")

	// Replace multiple dashes with a single dash (optional cleanup)
	reDash := regexp.MustCompile(`-+`)
	s = reDash.ReplaceAllString(s, "-")

	// Trim leading and trailing dashes or slashes to avoid issues
	s = strings.Trim(s, "-/")

	return s
}

// checkGHCLI checks if GitHub CLI is installed and configured
func checkGHCLI() error {
	// Check if gh is installed
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("GitHub CLI (gh) is not installed. Please install it first")
	}

	// Check if gh is authenticated
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("GitHub CLI is not configured. Please run 'gh auth login' first")
	}

	return nil
}

// PRInfo holds information extracted from a GitHub PR URL.
type PRInfo struct {
	URL      string // the full PR URL
	Owner    string
	Repo     string
	Number   string
	Branch   string // head branch name, populated by GetPRBranch
}

// ParsePRURL extracts owner, repo, and PR number from a GitHub PR URL.
// Returns nil if the text does not contain a valid PR URL.
func ParsePRURL(text string) *PRInfo {
	re := regexp.MustCompile(`https://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)
	match := re.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	return &PRInfo{
		URL:    match[0],
		Owner:  match[1],
		Repo:   match[2],
		Number: match[3],
	}
}

// GetPRBranch fetches the head branch name for a GitHub PR using the gh CLI.
func GetPRBranch(prURL string) (string, error) {
	if err := checkGHCLI(); err != nil {
		return "", err
	}
	cmd := exec.Command("gh", "pr", "view", prURL, "--json", "headRefName", "-q", ".headRefName")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get PR branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsGitRepo checks if the given path is within a git repository
func IsGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	return cmd.Run() == nil
}

func findGitRepoRoot(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find Git repository root from path: %s", path)
	}
	return strings.TrimSpace(string(out)), nil
}
