package overlay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverRepos(t *testing.T) {
	base := t.TempDir()

	// Create directories with .git (fake repos)
	for _, name := range []string{"repo-a", "repo-b"} {
		os.MkdirAll(filepath.Join(base, name, ".git"), 0755)
	}
	// Create a non-repo directory
	os.MkdirAll(filepath.Join(base, "not-a-repo"), 0755)
	// Create a file
	os.WriteFile(filepath.Join(base, "file.txt"), []byte("hi"), 0644)

	repos := DiscoverRepos(base)

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d: %v", len(repos), repos)
	}

	names := map[string]bool{}
	for _, r := range repos {
		names[filepath.Base(r)] = true
	}
	if !names["repo-a"] || !names["repo-b"] {
		t.Errorf("expected repo-a and repo-b, got %v", names)
	}
	if names["not-a-repo"] {
		t.Error("not-a-repo should not be discovered")
	}
}

func TestDiscoverRepos_nonexistentDir(t *testing.T) {
	repos := DiscoverRepos("/nonexistent/path/that/does/not/exist")
	if repos != nil {
		t.Errorf("expected nil for nonexistent dir, got %v", repos)
	}
}

func TestRepoPicker_SelectByName(t *testing.T) {
	rp := NewRepoPicker([]string{
		"/home/user/repos/alpha",
		"/home/user/repos/beta",
		"/home/user/repos/gamma",
	})

	t.Run("selects matching repo", func(t *testing.T) {
		found := rp.SelectByName("beta")
		if !found {
			t.Fatal("expected to find beta")
		}
		if got := rp.GetSelectedRepo(); got != "/home/user/repos/beta" {
			t.Errorf("selected = %q, want /home/user/repos/beta", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		found := rp.SelectByName("GAMMA")
		if !found {
			t.Fatal("expected to find gamma (case insensitive)")
		}
		if got := rp.GetSelectedRepo(); got != "/home/user/repos/gamma" {
			t.Errorf("selected = %q, want /home/user/repos/gamma", got)
		}
	})

	t.Run("no match returns false", func(t *testing.T) {
		found := rp.SelectByName("nonexistent")
		if found {
			t.Error("expected false for nonexistent repo")
		}
	})

	t.Run("clears filter on match", func(t *testing.T) {
		rp.filter = "some filter"
		rp.SelectByName("alpha")
		if rp.filter != "" {
			t.Errorf("expected filter to be cleared, got %q", rp.filter)
		}
	})
}

func TestRepoPicker_visibleItems(t *testing.T) {
	rp := NewRepoPicker([]string{
		"/repos/foo-bar",
		"/repos/baz-qux",
		"/repos/foo-baz",
	})

	t.Run("no filter returns all", func(t *testing.T) {
		items := rp.visibleItems()
		if len(items) != 3 {
			t.Errorf("expected 3 items, got %d", len(items))
		}
	})

	t.Run("filter by name", func(t *testing.T) {
		rp.filter = "foo"
		items := rp.visibleItems()
		if len(items) != 2 {
			t.Errorf("expected 2 items matching 'foo', got %d: %v", len(items), items)
		}
		rp.filter = ""
	})

	t.Run("filter no match", func(t *testing.T) {
		rp.filter = "zzz"
		items := rp.visibleItems()
		if len(items) != 0 {
			t.Errorf("expected 0 items, got %d", len(items))
		}
		rp.filter = ""
	})
}

func TestBranchPicker_SetDefaultBranch(t *testing.T) {
	bp := NewBranchPicker()

	t.Run("default applied when results arrive", func(t *testing.T) {
		bp.SetDefaultBranch("feature/my-branch")
		// Simulate results arriving
		bp.SetResults([]string{"main", "feature/my-branch", "develop"}, 0)

		got := bp.GetSelectedBranch()
		if got != "feature/my-branch" {
			t.Errorf("selected = %q, want feature/my-branch", got)
		}
	})

	t.Run("default not in results falls back to cursor", func(t *testing.T) {
		bp2 := NewBranchPicker()
		bp2.SetDefaultBranch("nonexistent-branch")
		bp2.SetResults([]string{"main", "develop"}, 0)

		// Cursor should be at 0 which is "New branch (from HEAD)" since showNewBranch is true
		// but nonexistent-branch isn't there, so default isn't applied
		got := bp2.GetSelectedBranch()
		// Should be empty string (New branch option) since default didn't match
		if got != "" {
			t.Errorf("expected empty (New branch), got %q", got)
		}
	})

	t.Run("default applied on later SetResults call", func(t *testing.T) {
		bp3 := NewBranchPicker()
		// Set default before any results
		bp3.SetDefaultBranch("hotfix")
		// First batch doesn't have it
		bp3.SetResults([]string{"main"}, 0)
		if got := bp3.GetSelectedBranch(); got == "hotfix" {
			t.Error("hotfix shouldn't be selected yet")
		}
		// Second batch has it
		bp3.filterVersion = 1
		bp3.SetResults([]string{"main", "hotfix"}, 1)
		if got := bp3.GetSelectedBranch(); got != "hotfix" {
			t.Errorf("selected = %q, want hotfix", got)
		}
	})
}
