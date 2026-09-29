package broker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"

	"gocc2cc/internal/core"
)

// These exercise a real broker over a real Unix socket. Registration, routing
// and delivery only work together, so testing them apart would prove little.

// shortTempDir is used instead of t.TempDir because a socket path has a hard
// length limit — around 104 bytes on macOS — and t.TempDir embeds the test
// name, which is enough to exceed it. The failure is an opaque "invalid
// argument" from bind, so it is worth avoiding by construction.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "g")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startBroker(t *testing.T) (*Broker, core.Paths) {
	t.Helper()
	paths := core.Paths{Root: shortTempDir(t)}
	b := New(paths)
	if err := b.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(b.Shutdown)
	return b, paths
}

// peer is a minimal agent: enough to register, send, and notice a push.
type peer struct {
	t      *testing.T
	ws     *websocket.Conn
	handle string
	in     chan core.Frame
}

func dialPeer(t *testing.T, socket string) *peer {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, "ws://gocc2cc/", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	p := &peer{t: t, ws: ws, in: make(chan core.Frame, 16)}
	go func() {
		for {
			_, data, err := ws.Read(context.Background())
			if err != nil {
				close(p.in)
				return
			}
			var f core.Frame
			if json.Unmarshal(data, &f) == nil {
				p.in <- f
			}
		}
	}()
	t.Cleanup(func() { _ = ws.Close(websocket.StatusNormalClosure, "") })
	return p
}

func (p *peer) write(f core.Frame) {
	p.t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		p.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.ws.Write(ctx, websocket.MessageText, data); err != nil {
		p.t.Fatalf("write: %v", err)
	}
}

func (p *peer) await(kind core.Kind) core.Frame {
	p.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case f, ok := <-p.in:
			if !ok {
				p.t.Fatalf("connection closed while waiting for %s", kind)
			}
			if f.Kind == kind {
				return f
			}
		case <-deadline:
			p.t.Fatalf("timed out waiting for %s", kind)
		}
	}
}

// awaitPeer waits for a message from another agent, stepping over anything
// gocc2cc sent itself — the registration greeting arrives first.
func (p *peer) awaitPeer() core.Frame {
	p.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case f, ok := <-p.in:
			if !ok {
				p.t.Fatal("connection closed while waiting for a peer message")
			}
			if f.Kind == core.KindPush && f.From != core.SystemSender {
				return f
			}
		case <-deadline:
			p.t.Fatal("timed out waiting for a peer message")
		}
	}
}

func (p *peer) register(app, topic string) string {
	p.t.Helper()
	p.write(core.Frame{Kind: core.KindRegister, ReqID: "reg", App: app, Topic: topic})
	f := p.await(core.KindRegistered)
	p.handle = f.Handle
	return f.Handle
}

func TestRegisterThenSeeEachOther(t *testing.T) {
	b, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	aliceHandle := alice.register("other/api", "api-refactor")

	bob := dialPeer(t, paths.Socket())
	bobHandle := bob.register("work/webapp", "docs-fix")

	if aliceHandle == bobHandle {
		t.Fatal("two agents must not share a handle")
	}
	if b.Count() != 2 {
		t.Fatalf("want 2 connected, got %d", b.Count())
	}

	bob.write(core.Frame{Kind: core.KindList, ReqID: "l"})
	f := bob.await(core.KindInstances)
	if len(f.Instances) != 2 {
		t.Fatalf("want 2 instances listed, got %d", len(f.Instances))
	}
	// Newest first — which falls out of the handle's id, not a separate field.
	if f.Instances[0].Handle != bobHandle {
		t.Fatalf("newest should be first; got %s", f.Instances[0].Handle)
	}
}

func TestMessageArrivesAndIsReportedDelivered(t *testing.T) {
	b, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	aliceHandle := alice.register("other/api", "api-refactor")
	bob := dialPeer(t, paths.Socket())
	bobHandle := bob.register("work/webapp", "docs-fix")

	alice.write(core.Frame{Kind: core.KindSend, ReqID: "s", To: bobHandle, Content: "migration done"})

	result := alice.await(core.KindSendResult)
	if !result.Delivered {
		t.Fatalf("expected delivery, got %q", result.Message)
	}

	push := bob.awaitPeer()
	if push.Content != "migration done" {
		t.Fatalf("content changed in flight: %q", push.Content)
	}
	// The sender is stamped by the broker, so an agent cannot claim to be
	// someone else.
	if push.From != aliceHandle {
		t.Fatalf("want from %s, got %s", aliceHandle, push.From)
	}

	var found bool
	for _, m := range b.Messages() {
		if m.Content == "migration done" && m.From == aliceHandle {
			found = true
		}
	}
	if !found {
		t.Fatalf("the message should be in the log, got %+v", b.Messages())
	}
}

func TestSenderCannotForgeItsIdentity(t *testing.T) {
	_, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	aliceHandle := alice.register("other/api", "api-refactor")
	bob := dialPeer(t, paths.Socket())
	bobHandle := bob.register("work/webapp", "docs-fix")

	alice.write(core.Frame{
		Kind: core.KindSend, ReqID: "s", To: bobHandle,
		From:    "someone/else::important::0192ffff", // ignored
		Content: "trust me",
	})
	alice.await(core.KindSendResult)

	push := bob.awaitPeer()
	if push.From != aliceHandle {
		t.Fatalf("the claimed sender should be ignored; got %s", push.From)
	}
}

// There are no queues. Sending to an agent that is not connected fails, and the
// sender is told why rather than left assuming success.
func TestSendingToNobodyFailsAndExplains(t *testing.T) {
	_, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	alice.register("other/api", "api-refactor")

	alice.write(core.Frame{
		Kind: core.KindSend, ReqID: "s",
		To:      "other/gone::vanished::0192ffffffff",
		Content: "anyone there?",
	})
	result := alice.await(core.KindSendResult)

	if result.Delivered {
		t.Fatal("delivery to an absent agent must fail")
	}
	if result.Message == "" {
		t.Fatal("a failed send must explain itself")
	}
}

// Addressing by topic when several agents share it must not pick one.
func TestAmbiguousAddressAsksRatherThanGuessing(t *testing.T) {
	_, paths := startBroker(t)

	one := dialPeer(t, paths.Socket())
	one.register("other/a", "default")
	two := dialPeer(t, paths.Socket())
	two.register("other/b", "default")

	sender := dialPeer(t, paths.Socket())
	sender.register("other/c", "sender")

	sender.write(core.Frame{Kind: core.KindSend, ReqID: "s", To: "default", Content: "hello"})
	result := sender.await(core.KindSendResult)

	if result.Delivered {
		t.Fatal("an ambiguous address must not be delivered to an arbitrary match")
	}
	if !contains(result.Message, "2 connected agents") {
		t.Fatalf("the failure should name the ambiguity: %q", result.Message)
	}
	if !contains(result.Message, one.handle) || !contains(result.Message, two.handle) {
		t.Fatalf("the failure should list the candidates: %q", result.Message)
	}
}

// A disconnected agent leaves the list at once — the operating system reports a
// closed socket immediately on a local connection, so nothing can go stale.
func TestDisconnectRemovesTheAgentImmediately(t *testing.T) {
	b, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	alice.register("other/api", "api-refactor")
	if b.Count() != 1 {
		t.Fatalf("want 1 connected, got %d", b.Count())
	}

	_ = alice.ws.Close(websocket.StatusNormalClosure, "")

	deadline := time.Now().Add(2 * time.Second)
	for b.Count() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if b.Count() != 0 {
		t.Fatalf("a closed connection should leave the list, still have %d", b.Count())
	}
}

// A killed agent is told why before its socket closes, so it can explain itself
// rather than discovering an opaque failure later.
func TestKillTellsTheAgentWhy(t *testing.T) {
	b, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	handle := alice.register("other/api", "api-refactor")

	if !b.Kill(handle) {
		t.Fatal("kill should find the agent")
	}

	f := alice.await(core.KindDisconnect)
	if f.Reason != core.ReasonKilled {
		t.Fatalf("want reason %q, got %q", core.ReasonKilled, f.Reason)
	}
}

// Shutdown says a different reason from a kill. Collapsing the two would make
// one case or the other behave wrongly on the agent's side.
func TestShutdownSaysSoRatherThanLookingLikeAKill(t *testing.T) {
	b, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	alice.register("other/api", "api-refactor")

	b.Shutdown()

	f := alice.await(core.KindDisconnect)
	if f.Reason != core.ReasonShutdown {
		t.Fatalf("want reason %q, got %q", core.ReasonShutdown, f.Reason)
	}
}

// A socket file left by an unclean exit must not block the next start, while a
// live one must. The difference is settled by evidence, not by the file's
// presence.
func TestStaleSocketIsReplacedButALiveOneIsNot(t *testing.T) {
	paths := core.Paths{Root: shortTempDir(t)}

	first := New(paths)
	if err := first.Listen(); err != nil {
		t.Fatalf("first listen: %v", err)
	}

	second := New(paths)
	if err := second.Listen(); err == nil {
		second.Shutdown()
		first.Shutdown()
		t.Fatal("a second broker must refuse to start while one is live")
	}

	first.Shutdown()

	// Leave a file behind as an unclean exit would.
	if err := writeFile(filepath.Join(paths.Root, "gocc2cc.sock")); err != nil {
		t.Fatal(err)
	}

	third := New(paths)
	if err := third.Listen(); err != nil {
		t.Fatalf("a stale socket should be replaced, got: %v", err)
	}
	third.Shutdown()
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func writeFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// A session must visibly join: the greeting lands in the agent's context, which
// is what makes gocc2cc's arrival apparent rather than something to take on
// trust — and it is where the window name is asked for.
func TestRegisteringIsGreeted(t *testing.T) {
	_, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	handle := alice.register("work/api", "")

	push := alice.await(core.KindPush)
	if push.From != core.SystemSender {
		t.Fatalf("the greeting should come from gocc2cc itself, got %q", push.From)
	}
	if !contains(push.Content, handle) {
		t.Errorf("the greeting should say what handle was assigned:\n%s", push.Content)
	}
	// The convention is answer-either-way: the name, or blank. Without the blank
	// case an agent whose window has no name simply says nothing, and gocc2cc
	// cannot tell that from an agent that ignored the question.
	if !contains(push.Content, `connect(topic: "the name")`) {
		t.Errorf("the greeting should show how to report a name:\n%s", push.Content)
	}
	if !contains(push.Content, `connect(topic: "")`) {
		t.Errorf("the greeting should show how to report having none:\n%s", push.Content)
	}
	// A resumed session may have been named long before this registration, so
	// the greeting must not claim the window is unnamed.
	for _, forbidden := range []string{"has no name", "is not named", "unnamed"} {
		if contains(push.Content, forbidden) {
			t.Errorf("must not assert the window is unnamed (%q):\n%s", forbidden, push.Content)
		}
	}
}

func TestGreetingConfirmsANamedWindow(t *testing.T) {
	_, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	alice.register("work/api", "auth-refactor")

	push := alice.await(core.KindPush)
	if !contains(push.Content, "auth-refactor") {
		t.Errorf("the greeting should echo the name it registered under:\n%s", push.Content)
	}
}

// A rename is something only the agent witnesses, so the obligation to report
// one has to be stated as standing for the whole session rather than as a
// request at startup.
func TestGreetingMakesRenameReportingAStandingRule(t *testing.T) {
	_, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	alice.register("work/api", "auth-refactor")

	push := alice.await(core.KindPush)
	for _, want := range []string{"FROM THEN ON", "every time", "renames", "connect("} {
		if !contains(push.Content, want) {
			t.Errorf("the greeting should say %q:\n%s", want, push.Content)
		}
	}
}

// Guidance that tells an agent it already knows the window name makes it go
// looking, and what it finds is a session label or a directory name. Registering
// one of those is worse than registering nothing: the operator sees a plausible
// wrong name instead of an obvious default.
func TestGreetingRulesOutTheLookalikeSources(t *testing.T) {
	_, paths := startBroker(t)

	alice := dialPeer(t, paths.Socket())
	alice.register("work/api", "")
	push := alice.await(core.KindPush)

	if !contains(push.Content, "/rename") {
		t.Errorf("the greeting should say where the name comes from:\n%s", push.Content)
	}
	for _, ruledOut := range []string{"session label", "list_instances", "directory name"} {
		if !contains(push.Content, ruledOut) {
			t.Errorf("the greeting should rule out %q as a source:\n%s", ruledOut, push.Content)
		}
	}
	// And it must never claim the agent already knows.
	for _, forbidden := range []string{"You know it", "YOURS TO REPORT"} {
		if contains(push.Content, forbidden) {
			t.Errorf("must not tell the agent it knows the name (%q):\n%s", forbidden, push.Content)
		}
	}
}
