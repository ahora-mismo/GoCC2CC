package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"gocc2cc/internal/core"
)

// liveCursor means nothing is selected. It is a cursor position rather than a
// mode, which is what makes returning to the live view the same gesture as
// scrolling to the top.
const liveCursor = -1

type messagesState struct {
	cursor int
	// frozen is the snapshot taken when a row was selected. While it is set the
	// list does not move: arrivals only change the counter, so nothing shifts
	// under someone reading.
	frozen  []core.Message
	reading bool
	// body scrolls the open message. A viewport rather than a line offset of
	// our own, so long lines wrap properly and the scroll maths is not ours to
	// get wrong.
	body viewport.Model
}

// openReading loads a message into the viewport. Done here rather than while
// rendering, so View has no side effects and the screens stay testable as
// pure rendering.
func (m *Model) openReading(msg core.Message) {
	m.msgs.body = viewport.New(m.readingWidth(), m.readingHeight())
	m.msgs.body.SetContent(msg.Content)
}

func (m Model) readingWidth() int {
	if w := m.width - 4; w > 20 {
		return w
	}
	return 20
}

func (m Model) readingHeight() int {
	if h := m.bodyHeight() - 5; h > 3 {
		return h
	}
	return 3
}

func (m Model) handleMessagesKey(key string) (tea.Model, tea.Cmd) {
	if m.msgs.reading {
		switch key {
		case "esc", "left", "backspace":
			m.msgs.reading = false
			return m, nil
		}
		// Scrolling is the viewport's job — it already accounts for wrapped
		// lines, which a line offset of our own would not.
		switch key {
		case "up":
			m.msgs.body.LineUp(1)
		case "down":
			m.msgs.body.LineDown(1)
		case "pgup":
			m.msgs.body.ViewUp()
		case "pgdown", " ":
			m.msgs.body.ViewDown()
		case "home", "g":
			m.msgs.body.GotoTop()
		case "end", "G":
			m.msgs.body.GotoBottom()
		}
		return m, nil
	}

	list := m.visibleMessages()

	switch key {
	case "up":
		if m.msgs.cursor == liveCursor {
			if len(list) > 0 {
				m.selectAt(0)
			}
		} else if m.msgs.cursor > 0 {
			m.msgs.cursor--
		}
	case "down":
		if m.msgs.cursor == liveCursor {
			if len(list) > 0 {
				m.selectAt(0)
			}
		} else if m.msgs.cursor < len(list)-1 {
			m.msgs.cursor++
		}
	case "left", "home":
		// Back to the top, and back to following. One gesture, no separate
		// "resume" key and no mode to get stuck in.
		m.msgs.cursor = liveCursor
		m.msgs.frozen = nil
	case "right", "end":
		if len(list) > 0 {
			m.selectAt(len(list) - 1)
		}
	case "enter":
		if m.msgs.cursor != liveCursor && m.msgs.cursor < len(list) {
			m.msgs.reading = true
			m.openReading(list[m.msgs.cursor])
		}
	}
	return m, nil
}

func (m *Model) selectAt(i int) {
	if m.msgs.frozen == nil {
		m.msgs.frozen = m.messages
	}
	m.msgs.cursor = i
}

// visibleMessages is the frozen snapshot while a row is selected, and the live
// log otherwise.
func (m Model) visibleMessages() []core.Message {
	if m.msgs.frozen != nil {
		return m.msgs.frozen
	}
	return m.messages
}

// arrivedSinceFreeze is how many messages came in while the list has been held
// still.
func (m Model) arrivedSinceFreeze() int {
	if m.msgs.frozen == nil {
		return 0
	}
	if d := len(m.messages) - len(m.msgs.frozen); d > 0 {
		return d
	}
	return 0
}

func (m Model) renderMessages() string {
	if m.msgs.reading {
		return m.renderReading()
	}

	list := m.visibleMessages()
	if len(list) == 0 {
		return styleEmpty.Render("No messages yet.")
	}

	// The count is shown rather than the limit: ageing means the list can hold
	// fewer than the maximum without anything being wrong.
	head := fmt.Sprintf("%d messages", len(list))
	if m.msgs.cursor == liveCursor {
		head += " · live"
	}

	const (
		wTime = 10
		wDate = 12
	)

	// Who is talking to whom is the point of this screen, and the app is the
	// part that identifies a session — several may share a topic, and every
	// unnamed one is called "default". So the correspondents get the space and
	// the message body takes what is left.
	wFrom, wTo, wBody := messageColumns(m.width)

	var b strings.Builder
	b.WriteString(styleSubhead.Render(head))
	b.WriteString("\n")
	b.WriteString(styleHeader.Render(
		"  " + pad("TIME", wTime) + pad("DATE", wDate) +
			party("FROM APP", "TOPIC", "ID", wFrom) + "  " +
			party("TO APP", "TOPIC", "ID", wTo) + "  MESSAGE",
	))
	b.WriteString("\n")

	if n := m.arrivedSinceFreeze(); n > 0 {
		b.WriteString(styleMuted.Render(fmt.Sprintf("▲ %d more above", n)))
		b.WriteString("\n")
	}

	rows := m.bodyHeight() - 3
	start := 0
	if m.msgs.cursor > liveCursor && m.msgs.cursor >= rows {
		start = m.msgs.cursor - rows + 1
	}

	shown := 0
	for idx := start; idx < len(list) && shown < rows; idx++ {
		msg := list[idx]
		cursor := "  "
		if idx == m.msgs.cursor {
			cursor = styleCursor.Render("❯ ")
		}
		line := cursor +
			pad(msg.At.Format("15:04:05"), wTime) +
			pad(msg.At.Format("2006-01-02"), wDate) +
			partyOf(msg.From, wFrom) + "  " +
			partyOf(msg.To, wTo) + "  " +
			truncate(oneLine(msg.Content), wBody)

		switch {
		case idx == m.msgs.cursor:
			b.WriteString(styleRowSelected.Render(line))
		case msg.IsSystem():
			b.WriteString(styleRow.Render(styleSystem.Render(line)))
		default:
			b.WriteString(styleRow.Render(line))
		}
		b.WriteString("\n")
		shown++
	}

	if remaining := len(list) - (start + shown); remaining > 0 {
		b.WriteString(styleMuted.Render(fmt.Sprintf("▼ %d more below", remaining)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderReading() string {
	list := m.visibleMessages()
	if m.msgs.cursor < 0 || m.msgs.cursor >= len(list) {
		return ""
	}
	msg := list[m.msgs.cursor]

	var b strings.Builder
	field := func(name, value string) {
		b.WriteString(styleRow.Render(styleFieldName.Render(pad(name, 8)) + value))
		b.WriteString("\n")
	}
	// Handles appear in full here — the list carries the trimmed form.
	field("from", msg.From)
	field("to", msg.To)
	field("at", msg.At.Format("15:04:05  2006-01-02"))
	b.WriteString(m.rule())
	b.WriteString("\n")
	b.WriteString(styleRow.Render(m.msgs.body.View()))
	return b.String()
}

// messageColumns divides the row between the two correspondents and the message.
//
// The split favours the correspondents deliberately: a truncated message is
// still readable by opening it, whereas a truncated handle cannot be identified
// at all.
func messageColumns(width int) (from, to, body int) {
	avail := width - (2 + 10 + 12) - 6 // cursor, time, date, separators
	if avail < 40 {
		avail = 40
	}
	from = avail * 2 / 5
	to = avail * 2 / 5
	body = avail - from - to
	if body < 10 {
		body = 10
	}
	return from, to, body
}

// party lays out one correspondent the way the connections list does — app,
// then topic, then the trimmed id — so the same session looks the same on both
// screens.
func party(app, topic, id string, width int) string {
	wApp, wTopic, wID := partyWidths(width)
	// cell keeps a space inside every column, so a name that fills its width
	// cannot run into the next one and read as a single word.
	return cell(app, wApp) + cell(topic, wTopic) + pad(id, wID)
}

// partyWidths divides a correspondent column between its three parts.
//
// They do not shrink together. The app says which project is talking and is the
// thing the list exists to show, so it gives up room last; the id only
// separates two sessions that are otherwise identical, so it gives up room
// first. Shrinking them proportionally would take the app away on an ordinary
// eighty-column terminal while the id kept every character it had.
func partyWidths(width int) (app, topic, id int) {
	app, topic, id = 14, 13, 9
	for _, part := range []struct {
		w, min int
		set    *int
	}{{id, 5, &id}, {topic, 5, &topic}, {app, 6, &app}} {
		over := app + topic + id - width
		if over <= 0 {
			break
		}
		if over > part.w-part.min {
			over = part.w - part.min
		}
		*part.set -= over
	}
	// Anything left over goes to the two names, the app first.
	if extra := width - (app + topic + id); extra > 0 {
		topic += extra / 2
		app += extra - extra/2
	}
	return app, topic, id
}

// partyOf renders a handle in that layout. The reserved system sender has no
// handle, so it occupies the app column alone.
func partyOf(handle string, width int) string {
	if handle == core.SystemSender || handle == "" {
		return pad(core.SystemSender, width)
	}
	h, err := core.ParseHandle(handle)
	if err != nil {
		return pad(handle, width)
	}
	return party(h.App, h.Topic, shortID(h, width), width)
}

// shortID trims the id to what its column can hold. The leading ellipsis says
// the id is a tail rather than a whole; when there is no room for it the column
// header carries that meaning instead, and the characters go to the id itself.
func shortID(h core.Handle, width int) string {
	_, _, wID := partyWidths(width)
	if wID >= 9 {
		return "..." + h.ShortID()
	}
	id := h.ShortID()
	if len(id) > wID {
		id = id[len(id)-wID:]
	}
	return id
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}
