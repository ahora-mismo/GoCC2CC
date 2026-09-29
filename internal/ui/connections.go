package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gocc2cc/internal/core"
)

type connectionsState struct {
	cursor int
	// armed is set by the first `k`. It belongs to the selected row: moving the
	// selection clears it, or `k` `↓` `k` would kill a row the operator never
	// confirmed — and it would look like a confirmed kill.
	armed bool
}

func (m Model) handleConnectionsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "shift+tab":
		if m.conn.cursor > 0 {
			m.conn.cursor--
		}
	case "down", "tab":
		if m.conn.cursor < len(m.instances)-1 {
			m.conn.cursor++
		}
	case "left", "home", "g":
		m.conn.cursor = 0
	case "right", "end", "G":
		m.conn.cursor = max(0, len(m.instances)-1)
	case "k":
		if len(m.instances) > 0 {
			m.conn.armed = true
		}
	}
	return m, nil
}

func (m *Model) killSelected() {
	if m.conn.cursor < 0 || m.conn.cursor >= len(m.instances) {
		return
	}
	m.src.Kill(m.instances[m.conn.cursor].Handle)
	m.refresh()
	if m.conn.cursor >= len(m.instances) {
		m.conn.cursor = max(0, len(m.instances)-1)
	}
}

func (m Model) renderKillConfirm() string {
	if m.conn.cursor >= len(m.instances) {
		return ""
	}
	i := m.instances[m.conn.cursor]
	h := core.Handle{App: i.App, Topic: i.Topic, ID: i.ID}
	return styleConfirm.Render(
		fmt.Sprintf("press k again to kill %s::%s   ·   any other key cancels", h.App, h.Short()),
	)
}

func (m Model) renderConnections() string {
	if len(m.instances) == 0 {
		return styleEmpty.Render("No sessions connected.\n\nStart Claude Code with gocc2cc named as a channel, then ask it to connect.")
	}

	const (
		wApp   = 20
		wTopic = 18
		wID    = 12
		wUp    = 8
	)

	var b strings.Builder
	b.WriteString(styleHeader.Render(
		"  " + pad("APP", wApp) + pad("TOPIC", wTopic) + pad("ID", wID) + pad("UP", wUp) + "IN/OUT",
	))
	b.WriteString("\n\n")

	rows := m.bodyHeight() - 2
	start := 0
	if m.conn.cursor >= rows {
		start = m.conn.cursor - rows + 1
	}

	for idx := start; idx < len(m.instances) && idx < start+rows; idx++ {
		i := m.instances[idx]
		h := core.Handle{App: i.App, Topic: i.Topic, ID: i.ID}

		cursor := "  "
		if idx == m.conn.cursor {
			cursor = styleCursor.Render("❯ ")
		}
		line := cursor +
			pad(i.App, wApp) +
			pad(i.Topic, wTopic) +
			pad("..."+h.ShortID(), wID) +
			pad(since(i.ConnectedAt), wUp) +
			fmt.Sprintf("%d/%d", i.Received, i.Sent)

		if idx == m.conn.cursor {
			b.WriteString(styleRowSelected.Render(line))
		} else {
			b.WriteString(styleRow.Render(line))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// since renders a duration the way a person reads one at a glance.
func since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
