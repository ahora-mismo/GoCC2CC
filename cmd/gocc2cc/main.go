// Command gocc2cc lets Claude Code sessions on one machine message each other.
//
// Run with no arguments it starts the broker and the interface. Run as
// `gocc2cc mcp` it is the shim Claude Code spawns per session — that mode is
// not meant to be run by hand.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"gocc2cc/internal/broker"
	"gocc2cc/internal/core"
	"gocc2cc/internal/mcp"
	"gocc2cc/internal/ui"
)

func main() {
	paths, err := core.DefaultPaths()
	if err != nil {
		fail(err)
	}

	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "":
		runBroker(paths)
	case "mcp":
		runShim(paths)
	case "hook":
		runHook()
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "gocc2cc: unknown command %q\n\n", mode)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`gocc2cc — message passing between Claude Code sessions on this machine

  gocc2cc        start the broker and the interface
  gocc2cc mcp    the MCP server Claude Code spawns; not run by hand
  gocc2cc hook   invoked by the SessionStart hook; not run by hand

See README.md for installation and the launch line.
`)
}

// runBroker starts the socket listener and the interface.
func runBroker(paths core.Paths) {
	// An incomplete install would otherwise present as a working app that never
	// does anything: the interface renders, the list stays empty, and nothing
	// explains why — because Claude Code has nothing to spawn.
	if checks, ok := paths.Preflight(); !ok {
		fmt.Fprint(os.Stderr, core.PreflightReport(checks, paths.Root))
		os.Exit(1)
	}

	b := broker.New(paths)
	if err := b.Listen(); err != nil {
		fail(err)
	}

	prog := tea.NewProgram(ui.New(b), tea.WithAltScreen())

	// Ctrl+C and SIGTERM take the same path as quitting from the interface, so
	// connected sessions are always told rather than left guessing.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		prog.Quit()
	}()

	_, err := prog.Run()

	// Tell every connected agent before going. Nothing is waited for: on a local
	// socket a successful write leaves the data readable by the other side, so
	// closing immediately does not discard it, and quitting cannot hang on a
	// session that has stopped reading.
	fmt.Println("Disconnecting agents…")
	b.Shutdown()

	if err != nil {
		fail(err)
	}
}

// runShim is the mode Claude Code spawns. stdout is the MCP stream, so nothing
// but protocol may be written there — every diagnostic goes to stderr.
func runShim(paths core.Paths) {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := mcp.Run(ctx, paths); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "gocc2cc: %v\n", err)
		os.Exit(1)
	}
}

// runHook is invoked by Claude Code's SessionStart hook. It records what the
// session is called so the shim can register under that name.
//
// A hook that fails noisily would interrupt the session for something entirely
// optional, so problems are reported to stderr and the exit status stays zero.
func runHook() {
	if len(os.Args) < 3 || os.Args[2] != "session-start" {
		fmt.Fprintln(os.Stderr, "gocc2cc: unknown hook")
		return
	}
	if err := core.RunSessionStartHook(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "gocc2cc: session-start hook: %v\n", err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "gocc2cc: %v\n", err)
	os.Exit(1)
}
