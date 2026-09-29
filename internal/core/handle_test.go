package core

import (
	"sort"
	"strings"
	"testing"
)

func TestNewHandleDefaultsEmptyTopic(t *testing.T) {
	h, err := NewHandle("work/webapp", "")
	if err != nil {
		t.Fatal(err)
	}
	if h.Topic != DefaultTopic {
		t.Fatalf("empty topic should become %q, got %q", DefaultTopic, h.Topic)
	}
}

func TestNewHandleKeepsGivenTopic(t *testing.T) {
	h, err := NewHandle("work/webapp", "auth-refactor")
	if err != nil {
		t.Fatal(err)
	}
	if h.Topic != "auth-refactor" {
		t.Fatalf("got %q", h.Topic)
	}
}

func TestNewHandleRejectsEmptyApp(t *testing.T) {
	if _, err := NewHandle("  ", "topic"); err == nil {
		t.Fatal("expected an error for an empty app name")
	}
}

func TestTopicCannotSmuggleSeparator(t *testing.T) {
	// A topic containing the separator would make the handle ambiguous to parse.
	h, err := NewHandle("app", "a::b")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.Topic, sep) {
		t.Fatalf("separator survived in topic: %q", h.Topic)
	}
	if _, err := ParseHandle(h.String()); err != nil {
		t.Fatalf("handle should still round-trip: %v", err)
	}
}

func TestHandleRoundTrip(t *testing.T) {
	h, err := NewHandle("work/webapp", "docs-fix")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseHandle(h.String())
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("round trip changed the handle:\n want %+v\n got  %+v", h, got)
	}
}

func TestParseHandleRejectsMalformed(t *testing.T) {
	for _, s := range []string{"", "a", "a::b", "a::b::c::d", "::b::c", "a::::c"} {
		if _, err := ParseHandle(s); err == nil {
			t.Errorf("expected %q to be rejected", s)
		}
	}
}

func TestShortShowsTrailingCharactersWithEllipsis(t *testing.T) {
	h := Handle{App: "other/api", Topic: "api-refactor", ID: "0192f3a1-b2c3-7def-8abc-123456789abc"}
	want := "api-refactor·...789abc"
	if h.Short() != want {
		t.Fatalf("want %q, got %q", want, h.Short())
	}
}

func TestAppFromDir(t *testing.T) {
	cases := map[string]string{
		"/Users/v/projects/work/webapp": "work/webapp",
		"/Users/v/work/infra":           "work/infra",
		"/single":                       "single",
	}
	for in, want := range cases {
		if got := AppFromDir(in); got != want {
			t.Errorf("AppFromDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// The ordering property is what lets connections be sorted newest-first with no
// separate timestamp field. If UUIDv7 generation ever loses its time prefix or
// its counter, this is the test that notices.
func TestIDsSortByCreationTime(t *testing.T) {
	var ids []string
	for i := 0; i < 50; i++ {
		id, err := uuidv7()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)

	for i := range ids {
		if ids[i] != sorted[i] {
			t.Fatalf("ids are not in creation order at %d:\n generated %s\n sorted    %s", i, ids[i], sorted[i])
		}
	}
}

func TestIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id, err := uuidv7()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = true
	}
}

func TestIDLooksLikeUUIDv7(t *testing.T) {
	id, err := uuidv7()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 {
		t.Fatalf("want 36 characters, got %d (%s)", len(id), id)
	}
	if id[14] != '7' {
		t.Fatalf("version nibble should be 7, got %c (%s)", id[14], id)
	}
	if c := id[19]; c != '8' && c != '9' && c != 'a' && c != 'b' {
		t.Fatalf("variant nibble should be 8-b, got %c (%s)", c, id)
	}
}

func TestSystemSenderCannotCollideWithAHandle(t *testing.T) {
	// System notices rely on this: a real handle always contains the separator.
	if strings.Contains(SystemSender, sep) {
		t.Fatal("the reserved system sender must not look like a handle")
	}
	if _, err := ParseHandle(SystemSender); err == nil {
		t.Fatal("the reserved system sender must not parse as a handle")
	}
}
