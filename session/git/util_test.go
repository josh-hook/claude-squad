package git

import (
	"testing"
)

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase string",
			input:    "feature",
			expected: "feature",
		},
		{
			name:     "string with spaces",
			input:    "new feature branch",
			expected: "new-feature-branch",
		},
		{
			name:     "mixed case string",
			input:    "FeAtUrE BrAnCh",
			expected: "feature-branch",
		},
		{
			name:     "string with special characters",
			input:    "feature!@#$%^&*()",
			expected: "feature",
		},
		{
			name:     "string with allowed special characters",
			input:    "feature/sub_branch.v1",
			expected: "feature/sub_branch.v1",
		},
		{
			name:     "string with multiple dashes",
			input:    "feature---branch",
			expected: "feature-branch",
		},
		{
			name:     "string with leading and trailing dashes",
			input:    "-feature-branch-",
			expected: "feature-branch",
		},
		{
			name:     "string with leading and trailing slashes",
			input:    "/feature/branch/",
			expected: "feature/branch",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "complex mixed case with special chars",
			input:    "USER/Feature Branch!@#$%^&*()/v1.0",
			expected: "user/feature-branch/v1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeBranchName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeBranchName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantNil    bool
		wantOwner  string
		wantRepo   string
		wantNumber string
		wantURL    string
	}{
		{
			name:       "valid PR URL",
			input:      "https://github.com/PolyAI-LDN/poly_cdktf_shared/pull/125",
			wantOwner:  "PolyAI-LDN",
			wantRepo:   "poly_cdktf_shared",
			wantNumber: "125",
			wantURL:    "https://github.com/PolyAI-LDN/poly_cdktf_shared/pull/125",
		},
		{
			name:       "PR URL embedded in text",
			input:      "Please review https://github.com/org/repo/pull/42 and fix the issue",
			wantOwner:  "org",
			wantRepo:   "repo",
			wantNumber: "42",
			wantURL:    "https://github.com/org/repo/pull/42",
		},
		{
			name:    "no PR URL",
			input:   "just some regular text",
			wantNil: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "github URL but not a PR",
			input:   "https://github.com/org/repo/issues/10",
			wantNil: true,
		},
		{
			name:       "PR URL with trailing text",
			input:      "https://github.com/org/repo/pull/99/files",
			wantOwner:  "org",
			wantRepo:   "repo",
			wantNumber: "99",
			wantURL:    "https://github.com/org/repo/pull/99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePRURL(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParsePRURL(%q) = %+v, want nil", tt.input, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParsePRURL(%q) = nil, want non-nil", tt.input)
			}
			if got.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", got.Owner, tt.wantOwner)
			}
			if got.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", got.Repo, tt.wantRepo)
			}
			if got.Number != tt.wantNumber {
				t.Errorf("Number = %q, want %q", got.Number, tt.wantNumber)
			}
			if got.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tt.wantURL)
			}
		})
	}
}

func TestIsGitRepo(t *testing.T) {
	t.Run("valid git repo", func(t *testing.T) {
		// The test itself runs inside the claude-squad repo
		if !IsGitRepo(".") {
			t.Error("expected current directory to be a git repo")
		}
	})

	t.Run("not a git repo", func(t *testing.T) {
		dir := t.TempDir()
		if IsGitRepo(dir) {
			t.Errorf("expected %s to not be a git repo", dir)
		}
	})
}

