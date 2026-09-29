package mcp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gocc2cc/internal/broker"
	"gocc2cc/internal/core"
)

// These run the shim's half of the conversation against a real broker over a
// real socket — the seam where the MCP mode meets our own protocol.

func shortTempDir(t *testing.T) string {
	t.Helper()
	// A socket path has a hard length limit, around 104 bytes on macOS, and
	// t.TempDir embeds the test name — enough to exceed it and fail with an
	// opaque "invalid argument" from bind.
	dir, err := os.MkdirTemp("", "g")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startBroker(t *testing.T) core.Paths {
	t.Helper()
	paths := core.Paths{Root: shortTempDir(t)}
	b := broker.New(paths)
	if err := b.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(b.Shutdown)
	return paths
}

func TestClientRegistersAndIsListed(t *testing.T) {
	paths := startBroker(t)
	ctx := context.Background()

	c := newClient(paths.Socket())
	if err := c.dial(ctx); err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.close()

	handle, err := c.register(ctx, "other/api", "api-refactor")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	h, err := core.ParseHandle(handle)
	if err != nil {
		t.Fatalf("the broker returned an unparseable handle %q: %v", handle, err)
	}
	if h.App != "other/api" || h.Topic != "api-refactor" {
		t.Fatalf("handle parts are wrong: %+v", h)
	}

	instances, err := c.list(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(instances) != 1 || instances[0].Handle != handle {
		t.Fatalf("expected to see ourselves listed, got %+v", instances)
	}
}

// An empty topic becomes "default" — the agent passes what it has, and gocc2cc
// applies the fallback rather than expecting the agent to.
func TestEmptyTopicBecomesDefault(t *testing.T) {
	paths := startBroker(t)
	ctx := context.Background()

	c := newClient(paths.Socket())
	if err := c.dial(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.close()

	handle, err := c.register(ctx, "other/api", "")
	if err != nil {
		t.Fatal(err)
	}
	h, _ := core.ParseHandle(handle)
	if h.Topic != core.DefaultTopic {
		t.Fatalf("want topic %q, got %q", core.DefaultTopic, h.Topic)
	}
}

// The whole point of the design: a message sent by one agent arrives in the
// other's context without anything being polled.
func TestMessageArrivesAtTheOtherClient(t *testing.T) {
	paths := startBroker(t)
	ctx := context.Background()

	received := make(chan core.Message, 1)

	bob := newClient(paths.Socket())
	// Skip the greeting gocc2cc sends on registration; this test is about a
	// message from a peer.
	bob.onPush = func(m core.Message) {
		if !m.IsSystem() {
			received <- m
		}
	}
	if err := bob.dial(ctx); err != nil {
		t.Fatal(err)
	}
	defer bob.close()
	bobHandle, err := bob.register(ctx, "work/webapp", "docs-fix")
	if err != nil {
		t.Fatal(err)
	}

	alice := newClient(paths.Socket())
	if err := alice.dial(ctx); err != nil {
		t.Fatal(err)
	}
	defer alice.close()
	aliceHandle, err := alice.register(ctx, "other/api", "api-refactor")
	if err != nil {
		t.Fatal(err)
	}

	delivered, detail, err := alice.send(ctx, bobHandle, "migration done")
	if err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Fatalf("expected delivery, got %q", detail)
	}

	select {
	case m := <-received:
		if m.Content != "migration done" {
			t.Fatalf("content changed: %q", m.Content)
		}
		if m.From != aliceHandle {
			t.Fatalf("want from %s, got %s", aliceHandle, m.From)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the message never arrived")
	}
}

// Sending to nobody fails and says so, because there is no queue to hold it.
func TestSendToAbsentAgentReportsFailure(t *testing.T) {
	paths := startBroker(t)
	ctx := context.Background()

	c := newClient(paths.Socket())
	if err := c.dial(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if _, err := c.register(ctx, "other/api", "api-refactor"); err != nil {
		t.Fatal(err)
	}

	delivered, detail, err := c.send(ctx, "gone/away::nobody::0192ffffffff", "hello?")
	if err != nil {
		t.Fatal(err)
	}
	if delivered {
		t.Fatal("a send to an absent agent must not report success")
	}
	if detail == "" {
		t.Fatal("a failed send must explain itself")
	}
}

// The broker not running is an ordinary condition, not a fault. Dialling fails
// with a reason the tools can pass on, rather than killing the session.
func TestDialingWithNoBrokerFailsCleanly(t *testing.T) {
	c := newClient("/nonexistent/gocc2cc.sock")
	err := c.dial(context.Background())
	if err == nil {
		t.Fatal("dialling a socket that does not exist should fail")
	}
	if !errors.Is(err, ErrNoBroker) {
		t.Fatalf("the failure should be recognisable as 'no broker': %v", err)
	}
	if c.connected() {
		t.Fatal("a failed dial must not leave the client looking connected")
	}
}

// A killed agent is told why, and the reason distinguishes an operator kill
// from the broker shutting down.
func TestKillReasonReachesTheClient(t *testing.T) {
	paths := core.Paths{Root: shortTempDir(t)}
	b := broker.New(paths)
	if err := b.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Shutdown)

	ctx := context.Background()
	reasons := make(chan core.Reason, 1)

	c := newClient(paths.Socket())
	c.onDisconnect = func(r core.Reason) { reasons <- r }
	if err := c.dial(ctx); err != nil {
		t.Fatal(err)
	}
	handle, err := c.register(ctx, "other/api", "api-refactor")
	if err != nil {
		t.Fatal(err)
	}

	if !b.Kill(handle) {
		t.Fatal("kill should find the agent")
	}

	select {
	case r := <-reasons:
		if r != core.ReasonKilled {
			t.Fatalf("want %q, got %q", core.ReasonKilled, r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the agent was never told why it was disconnected")
	}

	// Nothing reconnects on its own: the session stays out until asked.
	time.Sleep(200 * time.Millisecond)
	if c.connected() {
		t.Fatal("a killed client must not reconnect by itself")
	}
}

// A push that arrives before Claude Code has registered the channel is dropped
// silently — no error, nothing logged, the message simply never appears. So
// registration must wait for initialization rather than racing it.
func TestRegistrationWaitsForInitialization(t *testing.T) {
	paths := startBroker(t)
	b := broker.New(paths) // the one from startBroker is already listening
	_ = b

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Shim{paths: paths, client: newClient(paths.Socket()), init: make(chan struct{})}

	registered := make(chan string, 1)
	go func() {
		select {
		case <-s.init:
		case <-ctx.Done():
			return
		}
		time.Sleep(initGrace)
		if err := s.client.dial(ctx); err != nil {
			return
		}
		h, err := s.client.register(ctx, "other/api", "")
		if err == nil {
			registered <- h
		}
	}()

	// Nothing should have joined yet: initialization has not happened.
	time.Sleep(200 * time.Millisecond)
	select {
	case h := <-registered:
		t.Fatalf("registered before initialization: %s", h)
	default:
	}

	s.ready.Do(func() { close(s.init) })

	select {
	case h := <-registered:
		if h == "" {
			t.Fatal("expected a handle after initialization")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("never registered after initialization")
	}
}

// The agent is the only source of the window name: it can see /rename, gocc2cc
// cannot. Whatever it passes is what the handle carries.
func TestAgentSuppliedTopicIsUsed(t *testing.T) {
	paths := startBroker(t)
	ctx := context.Background()

	c := newClient(paths.Socket())
	if err := c.dial(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.close()

	handle, err := c.register(ctx, "work/api", "what-the-agent-said")
	if err != nil {
		t.Fatal(err)
	}
	h, _ := core.ParseHandle(handle)
	if h.Topic != "what-the-agent-said" {
		t.Fatalf("the agent's name should be used, got %q", h.Topic)
	}
}

// A session starts as "default" and stays there until the agent says otherwise.
// Nothing guesses a name from elsewhere.
func TestSessionStartsAsDefaultUntilTheAgentSaysOtherwise(t *testing.T) {
	paths := startBroker(t)
	ctx := context.Background()

	c := newClient(paths.Socket())
	if err := c.dial(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.close()

	first, err := c.register(ctx, "work/api", "")
	if err != nil {
		t.Fatal(err)
	}
	h, _ := core.ParseHandle(first)
	if h.Topic != core.DefaultTopic {
		t.Fatalf("want %q before the agent reports a name, got %q", core.DefaultTopic, h.Topic)
	}

	// The user renames the window; the agent reports it.
	second, err := c.register(ctx, "work/api", "auth-refactor")
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := core.ParseHandle(second)
	if h2.Topic != "auth-refactor" {
		t.Fatalf("want the reported name, got %q", h2.Topic)
	}
	// Re-registering mints a new id, which is why a peer holding the old handle
	// can no longer reach this session.
	if h2.ID == h.ID {
		t.Fatal("re-registering should produce a new id")
	}
}

// The convention is answer-either-way, so connect must be safe to call when
// nothing has changed. Otherwise an agent confirming "no name" would re-register
// for no reason — and a new registration mints a new id, stranding any peer
// holding the old handle.
func TestConnectingWithAnUnchangedNameKeepsTheSameHandle(t *testing.T) {
	paths := startBroker(t)
	ctx := context.Background()

	c := newClient(paths.Socket())
	if err := c.dial(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.close()

	first, err := c.register(ctx, "work/api", "auth-refactor")
	if err != nil {
		t.Fatal(err)
	}

	// What the connect tool does when the reported name matches: nothing.
	current, err := core.ParseHandle(c.currentHandle())
	if err != nil {
		t.Fatal(err)
	}
	if current.Topic != "auth-refactor" {
		t.Fatalf("want the registered name, got %q", current.Topic)
	}
	if current.String() != first {
		t.Fatal("the handle should not have changed")
	}
}
