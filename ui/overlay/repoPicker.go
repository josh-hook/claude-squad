package overlay

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RepoPicker is an embeddable component for selecting a git repository path.
// It shows discovered repos from a base directory, filterable by typing.
type RepoPicker struct {
	repos   []string // all available repo paths (discovered + recent, deduplicated)
	filter  string   // typed text to filter the list
	cursor  int      // index into visibleItems()
	focused bool
	width   int
}

// NewRepoPicker creates a new repo picker with the given list of available repos.
func NewRepoPicker(repos []string) *RepoPicker {
	return &RepoPicker{
		repos: repos,
	}
}

// DiscoverRepos scans baseDir for immediate subdirectories that contain a .git directory.
func DiscoverRepos(baseDir string) []string {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	var repos []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(baseDir, entry.Name())
		gitDir := filepath.Join(candidate, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			repos = append(repos, candidate)
		}
	}
	return repos
}

// Focus gives the repo picker focus.
func (rp *RepoPicker) Focus() {
	rp.focused = true
}

// Blur removes focus from the repo picker.
func (rp *RepoPicker) Blur() {
	rp.focused = false
}

// SetWidth sets the rendering width.
func (rp *RepoPicker) SetWidth(w int) {
	rp.width = w
}

// HandleKeyPress processes a key event. Returns (consumed, filterChanged).
func (rp *RepoPicker) HandleKeyPress(msg tea.KeyMsg) (consumed bool, filterChanged bool) {
	switch msg.Type {
	case tea.KeyUp:
		if rp.cursor > 0 {
			rp.cursor--
		}
		return true, false
	case tea.KeyDown:
		items := rp.visibleItems()
		if rp.cursor < len(items)-1 {
			rp.cursor++
		}
		return true, false
	case tea.KeyBackspace:
		if len(rp.filter) > 0 {
			runes := []rune(rp.filter)
			rp.filter = string(runes[:len(runes)-1])
			rp.cursor = 0
			return true, true
		}
		return true, false
	case tea.KeyRunes:
		rp.filter += string(msg.Runes)
		rp.cursor = 0
		return true, true
	case tea.KeySpace:
		rp.filter += " "
		rp.cursor = 0
		return true, true
	}
	return false, false
}

// visibleItems returns the list of items to display, filtered by the typed text.
// Matches against both the full path and the repo name (last path component).
func (rp *RepoPicker) visibleItems() []string {
	if rp.filter == "" {
		return rp.repos
	}
	lower := strings.ToLower(rp.filter)
	var items []string
	for _, repo := range rp.repos {
		name := strings.ToLower(filepath.Base(repo))
		full := strings.ToLower(repo)
		if strings.Contains(name, lower) || strings.Contains(full, lower) {
			items = append(items, repo)
		}
	}
	return items
}

// GetSelectedRepo returns the selected repo path, or empty string if nothing is selected.
func (rp *RepoPicker) GetSelectedRepo() string {
	items := rp.visibleItems()
	if rp.cursor >= 0 && rp.cursor < len(items) {
		return items[rp.cursor]
	}
	return ""
}

var (
	rpLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true)

	rpFilterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))

	rpSelectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("0"))

	rpDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	rpHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)
)

// Render renders the repo picker.
func (rp *RepoPicker) Render() string {
	var s strings.Builder
	s.WriteString(rpLabelStyle.Render("Repository"))
	if rp.focused {
		cursor := rp.filter + "\u2588"
		s.WriteString(rpFilterStyle.Render(" (filter: " + cursor + ")"))
	} else if rp.filter != "" {
		s.WriteString(rpDimStyle.Render(" (filter: " + rp.filter + ")"))
	}
	s.WriteString("\n\n")

	items := rp.visibleItems()
	if len(items) == 0 {
		s.WriteString(rpHintStyle.Render("  No matching repositories"))
		return s.String()
	}

	// Show max 5 visible items, windowed around cursor
	maxVisible := 5
	start := 0
	if rp.cursor >= maxVisible {
		start = rp.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		prefix := "  "
		label := items[i]
		if i == rp.cursor && rp.focused {
			prefix = "> "
			s.WriteString(rpSelectedStyle.Render(prefix + label))
		} else if i == rp.cursor {
			s.WriteString(prefix + label)
		} else {
			s.WriteString(rpDimStyle.Render(prefix + label))
		}
		if i < end-1 {
			s.WriteString("\n")
		}
	}

	return s.String()
}
