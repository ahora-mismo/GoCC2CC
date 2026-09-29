package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunSessionStartHook is the `gocc2cc hook session-start` subcommand, invoked by
// Claude Code when a session begins and again when one is cleared, compacted,
// resumed or forked.
//
// SessionStart is one of the few events whose stdout Claude Code adds to the
// conversation, so this is the one moment gocc2cc can speak to an agent before
// it is asked anything. That is the hook's whole purpose: to say what is
// available, and to ask the agent for the one thing only it knows — what this
// window is called.
func RunSessionStartHook(stdin io.Reader, stdout io.Writer) error {
	payload, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}

	var in struct {
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return fmt.Errorf("hook payload: %w", err)
	}
	if in.CWD == "" {
		return nil
	}

	// Leave a trace. A hook that does not run is indistinguishable from one that
	// runs and has its output ignored, and the difference decides where to look.
	traceHook(in.CWD)

	_, err = io.WriteString(stdout, SessionStartContext(in.CWD))
	return err
}

// traceHook appends one line per invocation. Failures are ignored: a diagnostic
// must never be the reason a session start fails.
func traceHook(cwd string) {
	paths, err := DefaultPaths()
	if err != nil {
		return
	}
	if err := paths.EnsureRoot(); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(paths.Root, "hook.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  session-start  %s\n", time.Now().Format(time.RFC3339), cwd)
}

// SessionStartContext is what an agent is told when its session begins.
//
// The window name is asked for rather than looked up. The agent can simply see
// it — the user types `/rename` in front of it — whereas gocc2cc would have to
// infer it from a file internal to Claude Code, and could not tell a name a
// person chose from one Claude Code invented for them.
//
// Deliberately short: it is added to every session whether or not gocc2cc is
// ever used.
func SessionStartContext(cwd string) string {
	app := AppFromDir(cwd)

	var b strings.Builder
	b.WriteString("gocc2cc is available in this session: you can message other Claude Code " +
		"sessions running on this machine.\n\n")
	fmt.Fprintf(&b, "This session registers as %s::%s::<id>, because gocc2cc does not know what "+
		"this window is called.\n\n", app, DefaultTopic)
	b.WriteString("Tell it, by calling connect with this window's name — the name set with " +
		"/rename, and nothing else:\n" +
		"  connect(topic: \"the name\")  if a /rename is visible in your conversation\n" +
		"  connect(topic: \"\")          if none is, including when the window was named " +
		"before this session was resumed\n\n" +
		"Never take the name from a session label, from list_instances, or from the project or " +
		"directory name — those are different things, and registering one is worse than " +
		"registering nothing. If you do not know the name, send blank and ask the user.\n\n" +
		"Call connect again every time the name changes: gocc2cc cannot see your conversation, " +
		"so you are the only one who can tell it.\n")
	b.WriteString("\nUse the gocc2cc skill when messaging another session, and call " +
		"list_instances() to see who is connected.\n")
	return b.String()
}
