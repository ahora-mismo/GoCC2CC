package core

import (
	"io"
	"strings"
	"testing"
)

// SessionStart is one of the few events whose stdout Claude Code adds to the
// conversation. If the hook prints nothing, nothing reaches the session and the
// agent never learns gocc2cc exists.
func TestHookSpeaksToTheSession(t *testing.T) {
	var out strings.Builder
	payload := `{"session_id":"s1","cwd":"/Users/v/work/api","hook_event_name":"SessionStart"}`

	if err := RunSessionStartHook(strings.NewReader(payload), &out); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if got == "" {
		t.Fatal("the hook must write context, or the session is told nothing")
	}
	if !strings.Contains(got, "work/api") {
		t.Errorf("the agent should be told which project this is:\n%s", got)
	}
	if !strings.Contains(got, "connect(") {
		t.Errorf("the agent should be told how to report its window name:\n%s", got)
	}
	if !strings.Contains(got, "gocc2cc skill") {
		t.Errorf("the agent should be pointed at the skill:\n%s", got)
	}
}

// The name is asked for, never inferred: the agent can see it, gocc2cc cannot.
func TestContextAsksTheAgentForTheWindowName(t *testing.T) {
	got := SessionStartContext("/Users/v/work/api")

	if !strings.Contains(got, "/rename") {
		t.Errorf("the context should say where the name comes from:\n%s", got)
	}
	if !strings.Contains(got, DefaultTopic) {
		t.Errorf("the context should say what it registers as meanwhile:\n%s", got)
	}
}

func TestHookIgnoresPayloadWithoutCwd(t *testing.T) {
	var out strings.Builder
	if err := RunSessionStartHook(strings.NewReader(`{"session_id":"x"}`), &out); err != nil {
		t.Fatalf("want a quiet no-op, got %v", err)
	}
	if out.String() != "" {
		t.Error("nothing useful to say means saying nothing")
	}
}

func TestHookRejectsMalformedPayload(t *testing.T) {
	if err := RunSessionStartHook(strings.NewReader("not json"), io.Discard); err == nil {
		t.Fatal("a malformed payload should be reported")
	}
}

// The startup context must not assert the agent knows the name, and must rule
// out the sources that look like one.
func TestContextDoesNotInviteGuessing(t *testing.T) {
	got := SessionStartContext("/Users/v/work/api")

	for _, ruledOut := range []string{"session label", "list_instances", "directory name"} {
		if !strings.Contains(got, ruledOut) {
			t.Errorf("should rule out %q as a source:\n%s", ruledOut, got)
		}
	}
	if !strings.Contains(got, "resumed") {
		t.Errorf("should cover the case where the name predates the session:\n%s", got)
	}
}
