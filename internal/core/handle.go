// Package core holds the vocabulary shared by both run modes: the handle that
// identifies an agent, the frames that travel over the socket, and the layout
// of ~/.gocc2cc.
package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultTopic is used when an agent connects without a window name.
const DefaultTopic = "default"

// SystemSender is the reserved sender for notices originating from gocc2cc
// itself rather than from a peer. A real handle always contains "::", so the
// two can never be confused.
const SystemSender = "gocc2cc"

const sep = "::"

// shortIDLen is how many trailing characters of the id lists display. The tail
// is the random half of a UUIDv7 — the leading half is a timestamp, shared
// between agents that started close together, so a prefix would not tell them
// apart.
const shortIDLen = 6

// Handle identifies one connected agent: which project it is working in, what
// the user named its window, and a unique id.
type Handle struct {
	App   string // "<parent>/<current>" of the agent's working directory
	Topic string // the window name, or DefaultTopic
	ID    string // UUIDv7 — unique, and ordered by creation time
}

// NewHandle builds a handle, substituting DefaultTopic for an empty topic and
// generating a fresh id.
func NewHandle(app, topic string) (Handle, error) {
	if strings.TrimSpace(app) == "" {
		return Handle{}, errors.New("app name is required")
	}
	id, err := uuidv7()
	if err != nil {
		return Handle{}, err
	}
	return Handle{App: app, Topic: normaliseTopic(topic), ID: id}, nil
}

func normaliseTopic(topic string) string {
	t := strings.TrimSpace(topic)
	if t == "" {
		return DefaultTopic
	}
	return strings.ReplaceAll(t, sep, "-")
}

// String is the full handle, and the form used for addressing.
func (h Handle) String() string {
	return h.App + sep + h.Topic + sep + h.ID
}

// Short is the form lists display: the topic, then an ellipsis and the last few
// characters of the id.
func (h Handle) Short() string {
	return h.Topic + "·..." + h.ShortID()
}

// ShortID is the trailing slice of the id shown in lists.
func (h Handle) ShortID() string {
	if len(h.ID) <= shortIDLen {
		return h.ID
	}
	return h.ID[len(h.ID)-shortIDLen:]
}

// ParseHandle reads a handle back from its string form.
func ParseHandle(s string) (Handle, error) {
	parts := strings.Split(s, sep)
	if len(parts) != 3 {
		return Handle{}, fmt.Errorf("malformed handle %q", s)
	}
	for _, p := range parts {
		if p == "" {
			return Handle{}, fmt.Errorf("malformed handle %q", s)
		}
	}
	return Handle{App: parts[0], Topic: parts[1], ID: parts[2]}, nil
}

// AppFromDir derives the app name from a working directory: the parent
// directory and the directory itself, e.g. "work/webapp". Two path segments
// because a bare directory name is often ambiguous — many checkouts contain an
// "api" or a "web".
func AppFromDir(dir string) string {
	dir = filepath.Clean(dir)
	base := filepath.Base(dir)
	parent := filepath.Base(filepath.Dir(dir))
	if parent == "." || parent == string(filepath.Separator) || parent == base {
		return base
	}
	return parent + "/" + base
}

// AppFromCwd derives the app name from the current working directory.
func AppFromCwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return AppFromDir(dir)
}

var mono struct {
	sync.Mutex
	ms      uint64
	counter uint16
}

// uuidv7 generates a UUIDv7: 48 bits of millisecond timestamp, a 12-bit
// counter, then randomness.
//
// The counter is what makes ordering total rather than merely approximate.
// Timestamps alone leave ids generated within the same millisecond to be
// ordered by their random tails, which is to say arbitrarily — and connections
// are sorted by id precisely so that no separate timestamp is needed.
// Incrementing a counter within a millisecond, and re-seeding it randomly when
// the millisecond advances, is the monotonic form described by RFC 9562.
func uuidv7() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	ms := uint64(time.Now().UnixMilli())

	mono.Lock()
	switch {
	case ms > mono.ms:
		mono.ms = ms
		// Seed from the random bytes, leaving headroom so a busy millisecond
		// cannot overflow into the next one.
		mono.counter = (uint16(b[6])<<8 | uint16(b[7])) & 0x07ff
	case ms == mono.ms:
		mono.counter++
		if mono.counter > 0x0fff {
			// More than 4096 ids in one millisecond: step the timestamp
			// forward rather than repeat ourselves.
			mono.ms++
			mono.counter = 0
		}
	default:
		// The clock went backwards. Keep our own notion of time moving forward,
		// since ordering matters more here than wall-clock accuracy.
		mono.counter++
	}
	counter := mono.counter
	ms = mono.ms
	mono.Unlock()

	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	b[6] = 0x70 | byte(counter>>8&0x0f) // version 7, then the counter's high nibble
	b[7] = byte(counter)
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant

	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
