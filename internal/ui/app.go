// Package ui is interface logic only. It owns what the operator is looking at
// — which tab, which row, whether a kill is armed, how far the reading view has
// scrolled — and renders whatever it is handed. It owns no application state:
// connections, message history and routing all live in the broker. The
// interface asks for a kill; it does not perform one.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gocc2cc/internal/core"
)

// Source is what the interface needs from the broker. Stated as an interface so
// the screens can be exercised without a running broker.
type Source interface {
	Instances() []core.Instance
	Messages() []core.Message
	Kill(handle string) bool
}

type tab int

const (
	tabConnections tab = iota
	tabMessages
)

// refreshInterval is how often the view is rebuilt. A tick is needed regardless
// of arriving traffic, because the connections list shows how long each agent
// has been connected.
const refreshInterval = time.Second

type tickMsg time.Time

// noticeDuration is how long the access notice stays on screen. Long enough to
// be read on the way past, short enough not to become furniture.
const noticeDuration = 60 * time.Second

// accessNotice states the security model plainly, because it is not obvious
// from using the app: someone who has not thought about it would reasonably
// assume agents only hear from each other.
const accessNotice = "No authentication — any process running as you can send messages to your sessions."

// Model is the whole interface.
type Model struct {
	src    Source
	tab    tab
	width  int
	height int

	instances []core.Instance
	messages  []core.Message

	conn connectionsState
	msgs messagesState

	started time.Time
	// quitArmed is set by the first `q`, the same way a kill is armed. Quitting
	// disconnects every session, so it is worth one deliberate confirmation.
	quitArmed bool
	quitting  bool
}

// New builds the interface over a broker.
func New(src Source) Model {
	m := Model{src: src, started: time.Now()}
	m.msgs.cursor = liveCursor
	m.refresh()
	return m
}

func (m Model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) refresh() {
	m.instances = m.src.Instances()
	m.messages = m.src.Messages()

	if m.conn.cursor >= len(m.instances) {
		m.conn.cursor = max(0, len(m.instances)-1)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.msgs.reading {
			m.msgs.body.Width, m.msgs.body.Height = m.readingWidth(), m.readingHeight()
		}
		return m, nil

	case tickMsg:
		m.refresh()
		return m, tick()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// An armed confirmation swallows the next keystroke whatever it is: the same
	// key again confirms, anything else cancels and does nothing further. One
	// extra press is a small price for never having a keystroke both cancel and
	// act.
	if m.conn.armed {
		m.conn.armed = false
		if key == "k" {
			m.killSelected()
		}
		return m, nil
	}
	if m.quitArmed {
		m.quitArmed = false
		if key == "q" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	switch key {
	case "ctrl+c":
		// The universal escape hatch stays immediate — someone reaching for it
		// wants out now, and will not thank us for a prompt.
		m.quitting = true
		return m, tea.Quit
	case "q":
		m.quitArmed = true
		return m, nil
	case "1":
		m.tab = tabConnections
		return m, nil
	case "2":
		m.tab = tabMessages
		return m, nil
	}

	if m.tab == tabConnections {
		return m.handleConnectionsKey(key)
	}
	return m.handleMessagesKey(key)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	if notice := m.renderNotice(); notice != "" {
		b.WriteString(notice)
		b.WriteString("\n")
	}
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	b.WriteString(m.rule())
	b.WriteString("\n")

	if m.tab == tabConnections {
		b.WriteString(m.renderConnections())
	} else {
		b.WriteString(m.renderMessages())
	}

	b.WriteString("\n")
	b.WriteString(m.rule())
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

// renderNotice shows the access notice for the first minute, then stops.
func (m Model) renderNotice() string {
	if time.Since(m.started) > noticeDuration {
		return ""
	}
	// Wrap to the terminal, or the notice runs off the edge of a narrow one.
	w := m.width
	if w <= 0 {
		w = 80
	}
	return styleNotice.Width(w).Render(accessNotice)
}

func (m Model) renderTabs() string {
	name := func(t tab, label string) string {
		if m.tab == t {
			return styleTabActive.Render("[" + label + "]")
		}
		return styleTabInactive.Render("[" + label + "]")
	}
	// Shortcuts live in the footer, never in the tab titles.
	return styleTabBar.Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			name(tabConnections, "connections"),
			name(tabMessages, "messages"),
		),
	)
}

func (m Model) rule() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	return styleRule.Render(strings.Repeat("─", w))
}

func (m Model) renderFooter() string {
	if m.conn.armed {
		return m.renderKillConfirm()
	}
	if m.quitArmed {
		n := len(m.instances)
		what := fmt.Sprintf("%d sessions", n)
		if n == 1 {
			what = "1 session"
		}
		return styleConfirm.Render(
			fmt.Sprintf("press q again to quit — %s will be disconnected   ·   any other key cancels", what),
		)
	}

	type binding struct{ key, label string }
	bindings := []binding{
		{"1", "connections"},
		{"2", "messages"},
	}
	if m.tab == tabConnections {
		if len(m.instances) > 0 {
			bindings = append(bindings,
				binding{"↑/↓", "move"},
				binding{"k", "kill"},
			)
		}
	} else if m.msgs.reading {
		bindings = append(bindings,
			binding{"↑/↓", "scroll"},
			binding{"esc/←", "back"},
		)
	} else if len(m.messages) > 0 {
		bindings = append(bindings,
			binding{"↑/↓", "move"},
			binding{"←", "top"},
			binding{"→", "bottom"},
			binding{"⏎", "open"},
		)
	}
	bindings = append(bindings, binding{"q", "quit"})

	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		parts = append(parts, styleFooterKey.Render(b.key)+" "+b.label)
	}
	return styleFooter.Render(strings.Join(parts, "   "))
}

// bodyHeight is how many rows the list area has, once the tab bar, rules and
// footer are accounted for.
func (m Model) bodyHeight() int {
	h := m.height - 6
	if h < 3 {
		return 3
	}
	return h
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// truncate shortens text to fit a column, marking that it did so.
func truncate(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	if len(runes) <= w {
		return s
	}
	return string(runes[:w-1]) + "…"
}

// cell is pad for a column with a neighbour: the content is kept one short of
// the width so there is always a gap after it.
func cell(s string, w int) string {
	if w <= 1 {
		return pad(s, w)
	}
	return pad(truncate(s, w-1), w)
}

// pad makes a column a fixed width so rows line up.
func pad(s string, w int) string {
	s = truncate(s, w)
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
