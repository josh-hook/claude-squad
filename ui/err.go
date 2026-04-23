package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type ErrBox struct {
	height, width int
	err           error
}

var errStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#FF0000",
	Dark:  "#FF0000",
})

func NewErrBox() *ErrBox {
	return &ErrBox{}
}

func (e *ErrBox) SetError(err error) {
	e.err = err
}

func (e *ErrBox) Clear() {
	e.err = nil
}

func (e *ErrBox) SetSize(width, height int) {
	e.width = width
	e.height = height
}

func (e *ErrBox) String() string {
	if e.err == nil {
		return lipgloss.Place(e.width, e.height, lipgloss.Center, lipgloss.Center, "")
	}

	maxWidth := e.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}

	// Word-wrap the error message to fit within the available width.
	msg := e.err.Error()
	var wrapped []string
	for _, line := range strings.Split(msg, "\n") {
		for runewidth.StringWidth(line) > maxWidth {
			wrapped = append(wrapped, runewidth.Truncate(line, maxWidth, ""))
			line = line[len(runewidth.Truncate(line, maxWidth, "")):]
		}
		wrapped = append(wrapped, line)
	}

	// Cap at 3 lines to avoid taking over the screen.
	const maxLines = 3
	if len(wrapped) > maxLines {
		wrapped = wrapped[:maxLines]
		wrapped[maxLines-1] = runewidth.Truncate(wrapped[maxLines-1], maxWidth-3, "") + "..."
	}

	rendered := errStyle.Render(strings.Join(wrapped, "\n"))
	return lipgloss.Place(e.width, e.height, lipgloss.Center, lipgloss.Center, rendered)
}
