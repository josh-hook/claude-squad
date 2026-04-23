package ui

import (
	"claude-squad/session"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

var (
	AdditionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	DeletionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	HunkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#0ea5e9"))

	fileBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	fileNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#c4b5fd"))

	statsBarStyle = lipgloss.NewStyle().
			MarginBottom(1)
)

type DiffPane struct {
	viewport viewport.Model
	diff     string
	stats    string
	width    int
	height   int
}

func NewDiffPane() *DiffPane {
	return &DiffPane{
		viewport: viewport.New(0, 0),
	}
}

func (d *DiffPane) SetSize(width, height int) {
	d.width = width
	d.height = height
	d.viewport.Width = width
	d.viewport.Height = height
	if d.diff != "" || d.stats != "" {
		d.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, d.stats, d.diff))
	}
}

func (d *DiffPane) SetDiff(instance *session.Instance) {
	centeredFallbackMessage := lipgloss.Place(
		d.width,
		d.height,
		lipgloss.Center,
		lipgloss.Center,
		"No changes",
	)

	if instance == nil || !instance.Started() {
		d.viewport.SetContent(centeredFallbackMessage)
		return
	}

	stats := instance.GetDiffStats()
	if stats == nil {
		centeredMessage := lipgloss.Place(
			d.width,
			d.height,
			lipgloss.Center,
			lipgloss.Center,
			"Setting up worktree...",
		)
		d.viewport.SetContent(centeredMessage)
		return
	}

	if stats.Error != nil {
		centeredMessage := lipgloss.Place(
			d.width,
			d.height,
			lipgloss.Center,
			lipgloss.Center,
			fmt.Sprintf("Error: %v", stats.Error),
		)
		d.viewport.SetContent(centeredMessage)
		return
	}

	if stats.IsEmpty() {
		d.stats = ""
		d.diff = ""
		d.viewport.SetContent(centeredFallbackMessage)
	} else {
		additions := AdditionStyle.Render(fmt.Sprintf(" +%d ", stats.Added))
		deletions := DeletionStyle.Render(fmt.Sprintf(" -%d ", stats.Removed))
		d.stats = statsBarStyle.Render(
			lipgloss.JoinHorizontal(lipgloss.Center, additions, " ", deletions))
		d.diff = formatDiff(stats.Content, d.width)
		d.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, d.stats, d.diff))
	}
}

func (d *DiffPane) String() string {
	return d.viewport.View()
}

func (d *DiffPane) ScrollUp() {
	d.viewport.LineUp(1)
}

func (d *DiffPane) ScrollDown() {
	d.viewport.LineDown(1)
}

// fileDiff represents the diff for a single file.
type fileDiff struct {
	name  string
	lines []string
}

// parseDiff splits raw unified diff output into per-file chunks,
// stripping the diff --git, --- a/..., +++ b/... metadata lines.
func parseDiff(raw string) []fileDiff {
	var files []fileDiff
	var current *fileDiff

	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			// Extract filename from "diff --git a/path b/path"
			parts := strings.SplitN(line, " b/", 2)
			name := ""
			if len(parts) == 2 {
				name = parts[1]
			}
			files = append(files, fileDiff{name: name})
			current = &files[len(files)-1]
			continue
		}
		if current == nil {
			continue
		}
		// Skip --- and +++ metadata lines
		if strings.HasPrefix(line, "--- a/") || strings.HasPrefix(line, "--- /dev/null") ||
			strings.HasPrefix(line, "+++ b/") || strings.HasPrefix(line, "+++ /dev/null") {
			continue
		}
		// Skip index lines like "index abc123..def456 100644"
		if strings.HasPrefix(line, "index ") {
			continue
		}
		// Skip mode lines
		if strings.HasPrefix(line, "old mode") || strings.HasPrefix(line, "new mode") ||
			strings.HasPrefix(line, "new file mode") || strings.HasPrefix(line, "deleted file mode") {
			continue
		}
		current.lines = append(current.lines, line)
	}
	return files
}

// formatDiff renders the full diff with per-file boxes.
func formatDiff(raw string, width int) string {
	files := parseDiff(raw)
	if len(files) == 0 {
		return ""
	}

	// Box width accounts for the viewport width minus some padding
	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	var sections []string
	for _, f := range files {
		header := fileNameStyle.Render(f.name)

		var body strings.Builder
		for _, line := range f.lines {
			if len(line) == 0 {
				body.WriteString("\n")
				continue
			}
			if strings.HasPrefix(line, "@@") {
				// Simplify hunk header: just show the @@ ... @@ part
				body.WriteString(HunkStyle.Render(line) + "\n")
			} else if line[0] == '+' {
				body.WriteString(AdditionStyle.Render(line) + "\n")
			} else if line[0] == '-' {
				body.WriteString(DeletionStyle.Render(line) + "\n")
			} else {
				body.WriteString(line + "\n")
			}
		}

		box := fileBoxStyle.Width(boxWidth).Render(
			lipgloss.JoinVertical(lipgloss.Left, header, body.String()))
		sections = append(sections, box)
	}

	return strings.Join(sections, "\n")
}
