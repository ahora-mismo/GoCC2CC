package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gocc2cc/internal/core"
)

// fakeSource stands in for the broker, which is the point of Source being an
// interface: the screens are testable with no socket and no processes.
type fakeSource struct {
	instances []core.Instance
	messages  []core.Message
	killed    []string
}

func (f *fakeSource) Instances() []core.Instance { return f.instances }
func (f *fakeSource) Messages() []core.Message   { return f.messages }
func (f *fakeSource) Kill(h string) bool {
	f.killed = append(f.killed, h)
	for i, in := range f.instances {
		if in.Handle == h {
			f.instances = append(f.instances[:i], f.instances[i+1:]...)
			return true
		}
	}
	return false
}

func instance(topic, id string) core.Instance {
	h := core.Handle{App: "work/webapp", Topic: topic, ID: id}
	return core.Instance{
		Handle: h.String(), App: h.App, Topic: h.Topic, ID: h.ID,
		ConnectedAt: time.Now().Add(-2 * time.Minute),
	}
}

func message(from, content string, at time.Time) core.Message {
	return core.Message{
		From:    core.Handle{App: "other/api", Topic: from, ID: "0192f3a1-b2c3-7def-8abc-1234567" + from}.String(),
		To:      core.Handle{App: "work/webapp", Topic: "docs-fix", ID: "0192f39c-1a2b-7cde-9fab-9876543a3f9c2"}.String(),
		Content: content,
		At:      at,
	}
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "up", "down", "left", "right", "enter", "esc":
			msg = tea.KeyMsg{Type: keyType(k)}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func keyType(k string) tea.KeyType {
	switch k {
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEsc
	}
	return tea.KeyRunes
}

func sized(m Model) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(Model)
}

func TestDigitsSwitchTabs(t *testing.T) {
	m := sized(New(&fakeSource{}))
	if m.tab != tabConnections {
		t.Fatal("should start on connections")
	}
	m = press(m, "2")
	if m.tab != tabMessages {
		t.Fatal("2 should select messages")
	}
	m = press(m, "1")
	if m.tab != tabConnections {
		t.Fatal("1 should select connections")
	}
}

// A single stray k must not kill anything. Two in a row must.
func TestKillNeedsTwoPresses(t *testing.T) {
	src := &fakeSource{instances: []core.Instance{instance("docs-fix", "0192aaa")}}
	m := sized(New(src))

	m = press(m, "k")
	if len(src.killed) != 0 {
		t.Fatal("one press must not kill")
	}
	if !m.conn.armed {
		t.Fatal("the first press should arm the confirm")
	}

	m = press(m, "k")
	if len(src.killed) != 1 {
		t.Fatalf("two presses should kill, killed %d", len(src.killed))
	}
}

// The dangerous case: if the armed state survived a selection change, this
// sequence would kill a row nobody confirmed — and it would look confirmed.
func TestMovingTheSelectionCancelsAnArmedKill(t *testing.T) {
	src := &fakeSource{instances: []core.Instance{
		instance("first", "0192bbb"),
		instance("second", "0192aaa"),
	}}
	m := sized(New(src))

	m = press(m, "k", "down", "k")

	if len(src.killed) != 0 {
		t.Fatalf("k, down, k must not kill anything; killed %v", src.killed)
	}
}

func TestAnyOtherKeyCancelsAnArmedKill(t *testing.T) {
	src := &fakeSource{instances: []core.Instance{instance("docs-fix", "0192aaa")}}
	m := sized(New(src))

	m = press(m, "k", "2") // switch tabs instead of confirming
	if len(src.killed) != 0 {
		t.Fatal("a different key must cancel the kill")
	}
	if m.conn.armed {
		t.Fatal("the confirm should be disarmed")
	}
	// The cancelling keystroke only cancels; it must not also act.
	if m.tab != tabConnections {
		t.Fatal("the cancelling key should not also switch tabs")
	}
}

// Selecting a row freezes the list, so nothing shifts under the reader. New
// arrivals only change the counter.
func TestSelectingAMessageFreezesTheList(t *testing.T) {
	now := time.Now()
	src := &fakeSource{messages: []core.Message{
		message("api", "newest", now),
		message("api", "older", now.Add(-time.Minute)),
	}}
	m := sized(New(src))
	m = press(m, "2", "down") // select the first row

	if m.msgs.frozen == nil {
		t.Fatal("selecting should freeze the list")
	}

	// Two more arrive while the operator is reading.
	src.messages = append([]core.Message{
		message("api", "brand new", now.Add(time.Minute)),
		message("api", "also new", now.Add(30*time.Second)),
	}, src.messages...)
	next, _ := m.Update(tickMsg(time.Now()))
	m = next.(Model)

	if got := len(m.visibleMessages()); got != 2 {
		t.Fatalf("the frozen list should still show 2 rows, shows %d", got)
	}
	if got := m.arrivedSinceFreeze(); got != 2 {
		t.Fatalf("want 2 counted as newly arrived, got %d", got)
	}
	if !strings.Contains(m.View(), "2 more above") {
		t.Error("the view should say how many arrived while frozen")
	}
}

// Returning to the top is the same gesture as returning to following: no
// separate resume key, and no mode to get stuck in.
func TestLeftReturnsToLiveFollowing(t *testing.T) {
	now := time.Now()
	src := &fakeSource{messages: []core.Message{message("api", "one", now)}}
	m := sized(New(src))
	m = press(m, "2", "down")

	if m.msgs.cursor == liveCursor {
		t.Fatal("selecting should move off the live position")
	}

	m = press(m, "left")

	if m.msgs.cursor != liveCursor {
		t.Fatal("left should return the cursor to live")
	}
	if m.msgs.frozen != nil {
		t.Fatal("left should unfreeze the list")
	}
	if !strings.Contains(m.View(), "live") {
		t.Error("the header should show the list is following again")
	}
}

func TestReadingViewOpensAndCloses(t *testing.T) {
	src := &fakeSource{messages: []core.Message{message("api", "line one\nline two", time.Now())}}
	m := sized(New(src))

	m = press(m, "2", "down", "enter")
	if !m.msgs.reading {
		t.Fatal("enter should open the reading view")
	}
	if !strings.Contains(m.View(), "line two") {
		t.Error("the reading view should show the whole message")
	}

	m = press(m, "esc")
	if m.msgs.reading {
		t.Fatal("esc should return to the list")
	}
}

// In the reading view left means back, not top-of-list. Same key, different
// screen.
func TestLeftMeansBackWhileReading(t *testing.T) {
	src := &fakeSource{messages: []core.Message{message("api", "body", time.Now())}}
	m := sized(New(src))
	m = press(m, "2", "down", "enter", "left")

	if m.msgs.reading {
		t.Fatal("left should leave the reading view")
	}
	if m.msgs.cursor == liveCursor {
		t.Fatal("leaving the reading view should keep the selection, not reset to live")
	}
}

// The header shows how many messages are held, not the limit, because ageing
// means the list can legitimately hold fewer.
func TestHeaderShowsTheRealCount(t *testing.T) {
	src := &fakeSource{messages: []core.Message{
		message("api", "a", time.Now()),
		message("api", "b", time.Now()),
		message("api", "c", time.Now()),
	}}
	m := sized(New(src))
	m = press(m, "2")

	if !strings.Contains(m.View(), "3 messages") {
		t.Errorf("header should show the real count:\n%s", m.View())
	}
}

func TestSystemNoticesAppearInTheList(t *testing.T) {
	src := &fakeSource{messages: []core.Message{{
		From: core.SystemSender, To: "work/webapp::docs-fix::0192aaa",
		Content: "Disconnected by the operator.", At: time.Now(),
	}}}
	m := sized(New(src))
	m = press(m, "2")

	view := m.View()
	if !strings.Contains(view, core.SystemSender) {
		t.Errorf("system notices should be listed:\n%s", view)
	}
	// The body is truncated by design — correspondents get the space — so check
	// only that it starts to appear.
	if !strings.Contains(view, "Disconnected") {
		t.Errorf("the notice text should be shown:\n%s", view)
	}
}

func TestEmptyStatesExplainThemselves(t *testing.T) {
	m := sized(New(&fakeSource{}))

	if !strings.Contains(m.View(), "No sessions connected") {
		t.Error("an empty connections list should say so")
	}
	m = press(m, "2")
	if !strings.Contains(m.View(), "No messages yet") {
		t.Error("an empty message list should say so")
	}
}

// Shortcuts belong in the footer, never in the tab titles.
func TestTabTitlesCarryNoShortcuts(t *testing.T) {
	m := sized(New(&fakeSource{}))
	tabs := m.renderTabs()

	if strings.Contains(tabs, "1") || strings.Contains(tabs, "2") {
		t.Errorf("tab titles should not show shortcuts: %q", tabs)
	}
	if !strings.Contains(m.renderFooter(), "connections") {
		t.Error("the footer should carry the bindings")
	}
}

func TestKillConfirmReplacesTheFooter(t *testing.T) {
	src := &fakeSource{instances: []core.Instance{instance("docs-fix", "0192aaa")}}
	m := sized(New(src))
	m = press(m, "k")

	footer := m.renderFooter()
	if !strings.Contains(footer, "press k again") {
		t.Errorf("the footer should become the confirm prompt: %q", footer)
	}
}

// The reading view scrolls with a viewport, so a very long line wraps instead
// of running off the edge — which a line offset of our own did not handle.
func TestReadingViewWrapsLongLines(t *testing.T) {
	long := strings.Repeat("the quick brown fox jumps over the lazy dog ", 12)
	src := &fakeSource{messages: []core.Message{message("api", long, time.Now())}}
	m := sized(New(src))
	m = press(m, "2", "down", "enter")

	view := m.View()
	for _, line := range strings.Split(view, "\n") {
		if got := len([]rune(line)); got > 140 {
			t.Fatalf("a line was not wrapped (%d runes): %q", got, line)
		}
	}
	if !strings.Contains(view, "quick brown fox") {
		t.Error("the message body should be visible")
	}
}

func TestReadingViewScrolls(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "line-"+string(rune('a'+i%26))+strings.Repeat("x", i%5))
	}
	body := strings.Join(lines, "\n")

	src := &fakeSource{messages: []core.Message{message("api", body, time.Now())}}
	m := sized(New(src))
	m = press(m, "2", "down", "enter")

	first := m.View()
	m = press(m, "down", "down", "down")
	scrolled := m.View()

	if first == scrolled {
		t.Error("scrolling should change what is shown")
	}
}

// The access model is not obvious from using the app, so it is stated — for a
// minute, then it gets out of the way.
func TestAccessNoticeShowsForOneMinute(t *testing.T) {
	m := sized(New(&fakeSource{}))
	shown := func() bool { return strings.Contains(m.View(), "No authentication") }

	if !shown() {
		t.Fatalf("the notice should be shown at startup:\n%s", m.View())
	}

	// Still there just before the minute is up.
	m.started = time.Now().Add(-(noticeDuration - time.Second))
	if !shown() {
		t.Error("the notice disappeared early")
	}

	// Gone just after — and the tick is what makes that happen on screen,
	// since nothing else redraws while the operator is idle.
	m.started = time.Now().Add(-(noticeDuration + time.Second))
	next, cmd := m.Update(tickMsg(time.Now()))
	m = next.(Model)
	if cmd == nil {
		t.Error("the tick should schedule the next one, or the view stops refreshing")
	}
	if shown() {
		t.Errorf("the notice should be gone after %s:\n%s", noticeDuration, m.View())
	}
}

// Quitting disconnects every session, so like a kill it takes two presses.
func TestQuitNeedsTwoPresses(t *testing.T) {
	m := sized(New(&fakeSource{instances: []core.Instance{instance("docs-fix", "0192aaa")}}))

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("one press must not quit")
	}
	if !m.quitArmed {
		t.Fatal("the first press should arm the confirm")
	}
	if !strings.Contains(m.renderFooter(), "press q again") {
		t.Errorf("the footer should become the confirm prompt: %q", m.renderFooter())
	}
	// The prompt should say what quitting costs.
	if !strings.Contains(m.renderFooter(), "disconnected") {
		t.Error("the prompt should say sessions will be disconnected")
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("two presses should quit")
	}
	if !m.quitting {
		t.Fatal("the model should be quitting")
	}
}

func TestAnyOtherKeyCancelsAnArmedQuit(t *testing.T) {
	m := sized(New(&fakeSource{}))
	m = press(m, "q", "2")

	if m.quitting {
		t.Fatal("a different key must cancel the quit")
	}
	if m.quitArmed {
		t.Fatal("the confirm should be disarmed")
	}
	// The cancelling keystroke only cancels; it must not also act.
	if m.tab != tabConnections {
		t.Fatal("the cancelling key should not also switch tabs")
	}
}

// Ctrl+C is the universal escape hatch and stays immediate — someone reaching
// for it wants out now.
func TestCtrlCQuitsImmediately(t *testing.T) {
	m := sized(New(&fakeSource{}))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)

	if cmd == nil || !m.quitting {
		t.Fatal("ctrl+c should quit without confirmation")
	}
}

// The two confirmations must not tangle: arming one and pressing the other key
// cancels rather than doing anything.
func TestArmedKillIsNotConfirmedByQuit(t *testing.T) {
	src := &fakeSource{instances: []core.Instance{instance("docs-fix", "0192aaa")}}
	m := sized(New(src))
	m = press(m, "k", "q")

	if len(src.killed) != 0 {
		t.Fatal("q must not confirm a kill")
	}
	if m.quitting || m.quitArmed {
		t.Fatal("the keystroke that cancelled the kill must not also start a quit")
	}
}

// Both correspondents are shown the way the connections tab shows a session —
// app, then topic, then the trimmed id, as three separate columns. The same
// session then looks the same on both screens, so it can be recognised without
// having to re-read it in a different shape.
func TestMessageListShowsBothPartiesInTheConnectionsFormat(t *testing.T) {
	from := core.Handle{App: "other/billing", Topic: "test-rename", ID: "0192f3a1-b2c3-7def-8abc-123456515243"}
	to := core.Handle{App: "work/webapp", Topic: "docs-fix", ID: "0192f39c-1a2b-7cde-9fab-987654a3f9c2"}
	src := &fakeSource{messages: []core.Message{{
		From: from.String(), To: to.String(), Content: "hello", At: time.Now(),
	}}}
	m := sized(New(src))
	m = press(m, "2")
	view := m.View()

	for _, h := range []core.Handle{from, to} {
		for _, want := range []string{h.App, h.Topic, "..." + h.ShortID()} {
			if !strings.Contains(view, want) {
				t.Errorf("the list should show %q for %s:\n%s", want, h.App, view)
			}
		}
	}
	// Separate columns, not one run-together field: the handle's own separator
	// must not appear in the row.
	if strings.Contains(view, "::") {
		t.Errorf("correspondents should be columns, not a joined handle:\n%s", view)
	}
}

// On a narrow terminal something has to give, and it must not be the app: that
// is the complaint this layout exists to answer. The id goes first — it only
// separates two sessions that are otherwise identical.
func TestTheAppKeepsItsRoomOnANarrowTerminal(t *testing.T) {
	for _, term := range []int{80, 100, 120} {
		from, _, _ := messageColumns(term)
		app, topic, id := partyWidths(from)
		if app < id {
			t.Errorf("at %d columns the app (%d) is narrower than the id (%d)", term, app, id)
		}
		if app < topic {
			t.Errorf("at %d columns the app (%d) is narrower than the topic (%d)", term, app, topic)
		}
		if got := app + topic + id; got > from {
			t.Errorf("at %d columns the parts total %d, over the column's %d", term, got, from)
		}
	}
}

// The correspondents must get more room than the message body: a truncated
// message can still be opened, a truncated handle cannot be identified.
func TestCorrespondentsGetMoreSpaceThanTheBody(t *testing.T) {
	from, to, body := messageColumns(120)
	if from <= body || to <= body {
		t.Fatalf("from=%d to=%d body=%d — the correspondents should be wider", from, to, body)
	}
	// And it must stay usable on a narrow terminal.
	from, to, body = messageColumns(60)
	if from < 10 || to < 10 || body < 5 {
		t.Fatalf("unusable at 60 columns: from=%d to=%d body=%d", from, to, body)
	}
}
