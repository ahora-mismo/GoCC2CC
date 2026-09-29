package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completeInstall builds a directory that passes preflight, so each test can
// break exactly one thing.
func completeInstall(t *testing.T) Paths {
	t.Helper()
	p := Paths{Root: t.TempDir()}

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(p.Bin(), "#!/bin/sh\n")
	write(p.MCPConfig(), `{"mcpServers":{}}`)
	write(p.Manifest(), `{"name":"gocc2cc"}`)
	write(p.MarketplaceManifest(), `{"name":"gocc2cc-local","plugins":[]}`)
	write(p.HooksConfig(), `{"hooks":{}}`)
	write(p.HookScript(), "#!/usr/bin/env bash\n")
	if err := os.Chmod(p.HookScript(), 0o700); err != nil {
		t.Fatal(err)
	}
	write(p.Skill(), "---\nname: gocc2cc\n---\n")
	return p
}

func TestPreflightPassesOnCompleteInstall(t *testing.T) {
	p := completeInstall(t)
	checks, ok := p.Preflight()
	if !ok {
		t.Fatalf("expected a complete install to pass:\n%s", PreflightReport(checks, p.Root))
	}
}

func TestPreflightFailsOnMissingBinary(t *testing.T) {
	p := completeInstall(t)
	if err := os.Remove(p.Bin()); err != nil {
		t.Fatal(err)
	}
	checks, ok := p.Preflight()
	if ok {
		t.Fatal("expected a missing binary to fail preflight")
	}
	if !reportMentions(checks, p.Bin()) {
		t.Fatal("the report should name the missing file")
	}
}

// A plugin that loads with no skill leaves the agent holding tools it does not
// know when to use — worse than an obvious failure, so an empty file must fail.
func TestPreflightFailsOnEmptySkill(t *testing.T) {
	p := completeInstall(t)
	if err := os.WriteFile(p.Skill(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	checks, ok := p.Preflight()
	if ok {
		t.Fatal("expected an empty skill file to fail preflight")
	}
	for _, c := range checks {
		if c.Path == p.Skill() && c.Detail == "" {
			t.Fatal("the failure should explain that the file is empty")
		}
	}
}

func TestPreflightFailsOnUnparseableManifest(t *testing.T) {
	p := completeInstall(t)
	if err := os.WriteFile(p.Manifest(), []byte("not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Preflight(); ok {
		t.Fatal("expected an unparseable manifest to fail preflight")
	}
}

func TestPreflightReportNamesEveryFile(t *testing.T) {
	p := completeInstall(t)
	_ = os.Remove(p.Manifest())
	checks, _ := p.Preflight()
	report := PreflightReport(checks, p.Root)

	// Listing individual paths is what makes the report actionable, and what
	// serves the manual-copy route rather than assuming a checkout.
	for _, want := range []string{p.Bin(), p.MCPConfig(), p.Manifest(), p.MarketplaceManifest(), p.HooksConfig(), p.Skill()} {
		if !strings.Contains(report, filepath.Base(want)) {
			t.Errorf("report should mention %s:\n%s", want, report)
		}
	}
	if !strings.Contains(report, "make install") {
		t.Error("report should say how to fix it")
	}
}

func TestEnsureRootIsOwnerOnly(t *testing.T) {
	p := Paths{Root: filepath.Join(t.TempDir(), "nested", "gocc2cc")}
	if err := p.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	// The socket has no other access control, so these permissions are the
	// whole boundary.
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("want mode 0700, got %o", perm)
	}
}

func TestEnsureRootTightensAnExistingLooseDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "gocc2cc")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := Paths{Root: root}
	if err := p.EnsureRoot(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("an existing directory should be tightened to 0700, got %o", perm)
	}
}

func reportMentions(checks []Check, path string) bool {
	for _, c := range checks {
		if c.Path == path {
			return true
		}
	}
	return false
}
