package broker

import (
	"fmt"
	"testing"
	"time"

	"gocc2cc/internal/core"
)

func msg(content string) core.Message {
	return core.Message{From: "a::b::c", To: "d::e::f", Content: content, At: time.Now()}
}

func TestNewestFirst(t *testing.T) {
	l := NewLog()
	l.Add(msg("first"))
	l.Add(msg("second"))
	l.Add(msg("third"))

	got := l.Messages()
	if len(got) != 3 {
		t.Fatalf("want 3 messages, got %d", len(got))
	}
	if got[0].Content != "third" {
		t.Fatalf("newest should be first, got %q", got[0].Content)
	}
	if got[2].Content != "first" {
		t.Fatalf("oldest should be last, got %q", got[2].Content)
	}
}

func TestDropsOldestWhenFull(t *testing.T) {
	l := NewLog()
	for i := 0; i < MaxMessages+10; i++ {
		l.Add(msg(fmt.Sprintf("m%d", i)))
	}

	if l.Len() != MaxMessages {
		t.Fatalf("want the log capped at %d, got %d", MaxMessages, l.Len())
	}

	got := l.Messages()
	// The ten oldest should be gone, not ten arbitrary ones.
	if got[0].Content != fmt.Sprintf("m%d", MaxMessages+9) {
		t.Fatalf("newest should be retained, got %q", got[0].Content)
	}
	if got[len(got)-1].Content != "m10" {
		t.Fatalf("oldest retained should be m10, got %q", got[len(got)-1].Content)
	}
}

// The distinction that matters: eviction must drop the oldest, not the least
// recently read. Reading the log must not change what gets dropped next.
func TestReadingDoesNotChangeWhatIsDropped(t *testing.T) {
	l := NewLog()
	for i := 0; i < MaxMessages; i++ {
		l.Add(msg(fmt.Sprintf("m%d", i)))
	}

	// Read the whole log several times; under a least-recently-used rule this
	// would reorder what is considered stale.
	for i := 0; i < 3; i++ {
		_ = l.Messages()
	}

	l.Add(msg("newcomer"))

	got := l.Messages()
	if l.Len() != MaxMessages {
		t.Fatalf("want %d, got %d", MaxMessages, l.Len())
	}
	if got[len(got)-1].Content != "m1" {
		t.Fatalf("m0 should have been dropped as the oldest, oldest retained is %q", got[len(got)-1].Content)
	}
}

func TestExpiredMessagesDisappear(t *testing.T) {
	l := NewLog()
	// Rebuild with a tiny lifetime rather than waiting 48 hours.
	l = newLogWithTTL(20 * time.Millisecond)

	l.Add(msg("transient"))
	if l.Len() != 1 {
		t.Fatalf("want 1 message, got %d", l.Len())
	}

	time.Sleep(60 * time.Millisecond)

	if l.Len() != 0 {
		t.Fatalf("want the message aged out, still have %d", l.Len())
	}
	if len(l.Messages()) != 0 {
		t.Fatal("an aged-out message should not be returned")
	}
}

func TestExpiryDoesNotStrandOrderEntries(t *testing.T) {
	l := newLogWithTTL(20 * time.Millisecond)
	for i := 0; i < 5; i++ {
		l.Add(msg(fmt.Sprintf("old%d", i)))
	}
	time.Sleep(60 * time.Millisecond)

	l.Add(msg("fresh"))

	got := l.Messages()
	if len(got) != 1 || got[0].Content != "fresh" {
		t.Fatalf("want only the fresh message, got %d: %+v", len(got), got)
	}
	// The internal ordering slice must have been pruned too, or it grows for
	// the life of the process.
	if len(l.order) != 1 {
		t.Fatalf("order slice should hold 1 entry, holds %d", len(l.order))
	}
}

func TestEmptyLog(t *testing.T) {
	l := NewLog()
	if l.Len() != 0 {
		t.Fatal("a new log should be empty")
	}
	if len(l.Messages()) != 0 {
		t.Fatal("a new log should return no messages")
	}
}

func TestSystemMessagesAreIdentifiable(t *testing.T) {
	l := NewLog()
	l.Add(core.Message{From: core.SystemSender, To: "a::b::c", Content: "shutting down", At: time.Now()})
	l.Add(msg("from a peer"))

	got := l.Messages()
	if got[0].IsSystem() {
		t.Fatal("the peer message should not be flagged as a system notice")
	}
	if !got[1].IsSystem() {
		t.Fatal("the gocc2cc notice should be flagged as a system notice")
	}
}
