package overlay

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TextOverlay represents a text screen overlay
type TextOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Callback function to be called when the overlay is dismissed
	OnDismiss func()
	// OnCopy is called when the user presses 'y' to copy content
	OnCopy func()
	// Content to display in the overlay
	content string

	width int
}

// NewTextOverlay creates a new text screen overlay with the given title and content
func NewTextOverlay(content string) *TextOverlay {
	return &TextOverlay{
		Dismissed: false,
		content:   content,
	}
}

// HandleKeyPress processes a key press and updates the state.
// Returns true if the overlay should be closed. Closes on Esc or q.
// 'y' triggers the OnCopy callback without closing.
func (t *TextOverlay) HandleKeyPress(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyEscape:
		t.Dismissed = true
		if t.OnDismiss != nil {
			t.OnDismiss()
		}
		return true
	default:
		switch msg.String() {
		case "q":
			t.Dismissed = true
			if t.OnDismiss != nil {
				t.OnDismiss()
			}
			return true
		case "y":
			if t.OnCopy != nil {
				t.OnCopy()
			}
			return false
		}
		return false
	}
}

// Render renders the text overlay
func (t *TextOverlay) Render(opts ...WhitespaceOption) string {
	// Create styles
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(t.width)

	// Apply the border style and return
	return style.Render(t.content)
}

func (t *TextOverlay) SetWidth(width int) {
	t.width = width
}
