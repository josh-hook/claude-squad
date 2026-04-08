package overlay

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RepoPicker is an embeddable component for selecting a git repository path.
// It displays a text input for typing a path and a list of recently-used repos.
type RepoPicker struct {
	recentRepos []string // known repo paths from existing instances
	filter      string   // typed text (path input)
	cursor      int      // index into visibleItems()
	focused     bool
	width       int
}

// NewRepoPicker creates a new repo picker with the given list of recent repos.
func NewRepoPicker(recentRepos []string) *RepoPicker {
	return &RepoPicker{
		recentRepos: recentRepos,
	}
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
func (rp *RepoPicker) visibleItems() []string {
	if rp.filter == "" {
		return rp.recentRepos
	}
	lower := strings.ToLower(rp.filter)
	var items []string
	for _, repo := range rp.recentRepos {
		if strings.Contains(strings.ToLower(repo), lower) {
			items = append(items, repo)
		}
	}
	return items
}

// GetSelectedRepo returns the selected repo path.
// If the cursor is on a recent repo, returns that. Otherwise returns the typed filter text.
// Expands ~ to $HOME.
func (rp *RepoPicker) GetSelectedRepo() string {
	items := rp.visibleItems()
	if rp.cursor >= 0 && rp.cursor < len(items) {
		return items[rp.cursor]
	}
	// No matching item — treat filter as a custom path
	return expandHome(rp.filter)
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
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
		s.WriteString(rpFilterStyle.Render(" (path: " + cursor + ")"))
	} else if rp.filter != "" {
		s.WriteString(rpDimStyle.Render(" (path: " + rp.filter + ")"))
	}
	s.WriteString("\n\n")

	items := rp.visibleItems()
	if len(items) == 0 && rp.filter == "" {
		s.WriteString(rpHintStyle.Render("  Type a path to a git repository"))
		return s.String()
	}

	if len(items) == 0 {
		s.WriteString(rpHintStyle.Render("  Will use: " + expandHome(rp.filter)))
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
