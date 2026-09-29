package broker

import (
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"

	"gocc2cc/internal/core"
)

const (
	// MaxMessages is how many the log holds before the oldest is dropped.
	MaxMessages = 100
	// MessageTTL is how long a message survives regardless of the limit, so a
	// quiet machine does not keep days-old conversation at the top of the list.
	MessageTTL = 48 * time.Hour
)

// Log is the message history the interface displays: the most recent
// MaxMessages, none older than MessageTTL.
//
// The library supplies the ageing. The limit is enforced here rather than by
// the library's own eviction, which drops the least recently *used* entry — the
// wrong rule for a list where nothing is ever read individually, and a rule
// that would silently keep whichever message happened to be looked at last.
type Log struct {
	mu    sync.Mutex
	cache *expirable.LRU[uint64, core.Message]
	order []uint64 // insertion order, oldest first
	next  uint64
}

// NewLog creates an empty log.
func NewLog() *Log {
	return &Log{
		// Size 0 leaves the library unbounded; the count is our business.
		cache: expirable.NewLRU[uint64, core.Message](0, nil, MessageTTL),
	}
}

// Add records a message, dropping the oldest if the log is already full.
func (l *Log) Add(m core.Message) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.dropExpiredLocked()

	for len(l.order) >= MaxMessages {
		oldest := l.order[0]
		l.order = l.order[1:]
		l.cache.Remove(oldest)
	}

	key := l.next
	l.next++
	l.cache.Add(key, m)
	l.order = append(l.order, key)
}

// Messages returns the log newest first, which is the order the interface
// displays and the order new arrivals appear at the top.
func (l *Log) Messages() []core.Message {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.dropExpiredLocked()

	out := make([]core.Message, 0, len(l.order))
	for i := len(l.order) - 1; i >= 0; i-- {
		if m, ok := l.cache.Get(l.order[i]); ok {
			out = append(out, m)
		}
	}
	return out
}

// Len is how many messages are currently held. It can be below MaxMessages
// because of ageing, which is why the interface shows the real count rather
// than stating the limit.
func (l *Log) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dropExpiredLocked()
	return len(l.order)
}

// dropExpiredLocked removes order entries the cache has already aged out. The
// cache expires them on its own schedule; this keeps our ordering in step
// without needing an eviction callback, which would mean re-entering this lock
// from the library's.
func (l *Log) dropExpiredLocked() {
	if len(l.order) == 0 {
		return
	}
	kept := l.order[:0]
	for _, k := range l.order {
		if _, ok := l.cache.Peek(k); ok {
			kept = append(kept, k)
		}
	}
	l.order = kept
}

// newLogWithTTL builds a log with a non-default lifetime. Used by tests so the
// ageing behaviour can be exercised without waiting MessageTTL.
func newLogWithTTL(ttl time.Duration) *Log {
	return &Log{cache: expirable.NewLRU[uint64, core.Message](0, nil, ttl)}
}
