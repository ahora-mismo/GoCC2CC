package broker

import (
	"fmt"
	"strings"
	"time"

	"gocc2cc/internal/core"
)

// Deliver pushes a message from one agent to another.
//
// There is no queue. Either the recipient is connected and the frame is written
// to its socket, or the send fails and the sender is told — which is why the
// write error is returned rather than swallowed: it is the only honest evidence
// that anything arrived.
func (b *Broker) Deliver(from, to, content string) (bool, string) {
	target, ok := b.registry.Lookup(to)
	if !ok {
		if hint := b.ambiguityHint(to); hint != "" {
			return false, hint
		}
		return false, fmt.Sprintf("no agent is connected with the handle %q", to)
	}

	msg := core.Message{From: from, To: to, Content: content, At: time.Now()}
	err := target.Send(core.Frame{
		Kind:    core.KindPush,
		From:    from,
		To:      to,
		Content: content,
		At:      msg.At,
	})
	if err != nil {
		return false, fmt.Sprintf("the recipient's connection failed while sending: %v", err)
	}

	b.log.Add(msg)
	b.registry.countSent(from)
	b.registry.countReceived(to)
	return true, ""
}

// Notify sends a message from gocc2cc itself to one agent. Such notices are
// identified by their sender rather than by a flag: a real handle always
// contains the separator, so the two can never be confused.
func (b *Broker) Notify(to, content string) {
	target, ok := b.registry.Lookup(to)
	if !ok {
		return
	}
	msg := core.Message{From: core.SystemSender, To: to, Content: content, At: time.Now()}
	_ = target.Send(core.Frame{
		Kind:    core.KindPush,
		From:    core.SystemSender,
		To:      to,
		Content: content,
		At:      msg.At,
	})
	b.log.Add(msg)
}

// welcome greets a newly registered agent.
//
// This is what makes a session visibly join: the message lands in the agent's
// context as a channel notification, so the operator sees gocc2cc arrive rather
// than having to take it on trust. It also asks for the one thing only the agent
// can supply — what the user named this window — which is why the greeting is a
// request rather than a pleasantry.
func (b *Broker) welcome(h core.Handle) {
	var text strings.Builder
	fmt.Fprintf(&text, "You are connected to gocc2cc as %s.\n\n", h)

	// Say plainly where the name comes from, and rule out the sources that look
	// like one but are not. Told it already knows the name, an agent goes
	// looking and finds a session label or a directory name — which is worse
	// than registering nothing, because it looks right.
	text.WriteString("REPLY NOW by calling connect with this window's name — the name set with " +
		"/rename, and nothing else:\n" +
		"  connect(topic: \"the name\")  if a /rename is visible in your conversation\n" +
		"  connect(topic: \"\")          if none is, including when the window was named " +
		"before this session was resumed\n\n" +
		"Never take the name from a session label, from list_instances, or from the project or " +
		"directory name. If you do not know it, send blank and ask the user.\n\n")

	if h.Topic != core.DefaultTopic {
		fmt.Fprintf(&text, "gocc2cc currently has it as %q. Confirm or correct that.\n\n", h.Topic)
	}

	// A rename is something only the agent witnesses — the user types it into
	// the conversation — and gocc2cc has no way to notice it, so the obligation
	// has to stand for the whole session rather than just this moment.
	text.WriteString("FROM THEN ON: call connect again every time the user renames this window. " +
		"Your handle carries the name, so it goes stale otherwise and the operator sees the " +
		"wrong one.\n\n")

	text.WriteString("Calling connect when nothing has changed is safe. Do not reply with a message.")

	b.Notify(h.String(), text.String())
}

// ambiguityHint explains what went wrong when an address names a topic that
// several agents share, rather than letting the caller guess. A message is
// never sent to an arbitrary match.
func (b *Broker) ambiguityHint(to string) string {
	// A bare topic is the likely mistake — a full handle has two separators.
	if strings.Contains(to, "::") {
		return ""
	}
	matches := b.registry.MatchTopic(to)
	switch len(matches) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%q is a topic, not a handle. Did you mean %s?", to, matches[0])
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q is a topic shared by %d connected agents. Ask the user which one, then send to its full handle:", to, len(matches))
		for _, m := range matches {
			fmt.Fprintf(&b, "\n  %s", m)
		}
		return b.String()
	}
}
