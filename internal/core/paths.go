package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dirMode keeps everything under ~/.gocc2cc readable only by its owner. The
// socket has no other access control, so these permissions are the boundary.
const dirMode = 0o700

// Paths is the layout of ~/.gocc2cc. Everything the app needs lives here, and
// nothing references the source checkout.
type Paths struct {
	Root string
}

// DefaultPaths resolves ~/.gocc2cc.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return Paths{Root: filepath.Join(home, ".gocc2cc")}, nil
}

func (p Paths) Bin() string { return filepath.Join(p.Root, "bin", "gocc2cc") }

// Socket is where the broker listens. GOCC2CC_SOCKET overrides it, which the
// integration tests use to point the shim at a socket that does not exist and
// check it survives that rather than failing the session.
func (p Paths) Socket() string {
	if s := os.Getenv("GOCC2CC_SOCKET"); s != "" {
		return s
	}
	return filepath.Join(p.Root, "gocc2cc.sock")
}

// MarketplaceDir is what Claude Code is pointed at. A marketplace is simply a
// directory holding a manifest that lists plugins — nothing is published
// anywhere, and this one lists exactly one.
func (p Paths) MarketplaceDir() string { return filepath.Join(p.Root, "marketplace") }
func (p Paths) MarketplaceManifest() string {
	return filepath.Join(p.MarketplaceDir(), ".claude-plugin", "marketplace.json")
}

// PluginDir is the plugin itself, inside the marketplace that lists it.
func (p Paths) PluginDir() string { return filepath.Join(p.MarketplaceDir(), "gocc2cc") }
func (p Paths) Manifest() string {
	return filepath.Join(p.PluginDir(), ".claude-plugin", "plugin.json")
}
func (p Paths) MCPConfig() string   { return filepath.Join(p.PluginDir(), ".mcp.json") }
func (p Paths) HooksConfig() string { return filepath.Join(p.PluginDir(), "hooks", "hooks.json") }
func (p Paths) HookScript() string {
	return filepath.Join(p.PluginDir(), "hooks", "session-start.sh")
}
func (p Paths) Skill() string {
	return filepath.Join(p.PluginDir(), "skills", "gocc2cc", "SKILL.md")
}

// EnsureRoot creates the directory tree if it is missing. The broker calls this
// as well as the installer, since it may be started in a state the installer
// did not leave.
func (p Paths) EnsureRoot() error {
	if err := os.MkdirAll(p.Root, dirMode); err != nil {
		return err
	}
	// MkdirAll leaves an existing directory's mode alone, so tighten it.
	return os.Chmod(p.Root, dirMode)
}

// Check is one preflight result.
type Check struct {
	Path   string
	OK     bool
	Detail string // why it failed, when it did
}

// Display renders the path with the home directory abbreviated, so messages
// read the way a person would write the path.
func (c Check) Display(root string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return c.Path
	}
	return strings.Replace(c.Path, home, "~", 1)
}

// Preflight verifies that an install is complete enough to be usable. An
// incomplete install otherwise presents as a working app that never does
// anything: the broker starts, the interface renders, and no agent ever
// appears, because Claude Code has nothing to spawn.
func (p Paths) Preflight() ([]Check, bool) {
	checks := []Check{
		mustExist(p.Bin()),
		mustExist(p.MCPConfig()),
		mustParseJSON(p.Manifest()),
		mustParseJSON(p.MarketplaceManifest()),
		mustParseJSON(p.HooksConfig()),
		mustBeExecutable(p.HookScript()),
		mustBeNonEmpty(p.Skill()),
	}
	ok := true
	for _, c := range checks {
		if !c.OK {
			ok = false
		}
	}
	return checks, ok
}

func mustExist(path string) Check {
	if _, err := os.Stat(path); err != nil {
		return Check{Path: path, Detail: "missing"}
	}
	return Check{Path: path, OK: true}
}

func mustBeNonEmpty(path string) Check {
	info, err := os.Stat(path)
	if err != nil {
		return Check{Path: path, Detail: "missing"}
	}
	if info.Size() == 0 {
		// A plugin that loads with no skill leaves the agent holding tools it
		// does not know when to use, which is worse than an obvious failure.
		return Check{Path: path, Detail: "present, empty"}
	}
	return Check{Path: path, OK: true}
}

// mustBeExecutable exists because a hook script without the execute bit fails
// at session-start time, far from anything that would explain it.
func mustBeExecutable(path string) Check {
	info, err := os.Stat(path)
	if err != nil {
		return Check{Path: path, Detail: "missing"}
	}
	if info.Mode().Perm()&0o111 == 0 {
		return Check{Path: path, Detail: "present, not executable"}
	}
	return Check{Path: path, OK: true}
}

func mustParseJSON(path string) Check {
	b, err := os.ReadFile(path)
	if err != nil {
		return Check{Path: path, Detail: "missing"}
	}
	if !looksLikeJSONObject(b) {
		return Check{Path: path, Detail: "present, not valid JSON"}
	}
	return Check{Path: path, OK: true}
}

func looksLikeJSONObject(b []byte) bool {
	s := strings.TrimSpace(string(b))
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
}

// PreflightReport renders the failure the app prints before exiting.
func PreflightReport(checks []Check, root string) string {
	var b strings.Builder
	b.WriteString("gocc2cc: install is incomplete\n\n")
	for _, c := range checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		line := fmt.Sprintf("  %s %s", mark, c.Display(root))
		if !c.OK && c.Detail != "" && c.Detail != "missing" {
			line += fmt.Sprintf("   (%s)", c.Detail)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nRun `make install` from the gocc2cc checkout to create them,\n")
	b.WriteString("or copy them into ~/.gocc2cc/ manually.\n")
	return b.String()
}
