# test/

The Venom suites. These are the integration tests; `make test` runs the unit
tests and does not touch this directory.

## Running them

```bash
make test-integration              # builds, then runs every suite
bash test/run-venom.sh local       # same thing, if the binary is already built
bash test/run-venom.sh local 01-mcp-surface.yml   # just one suite
```

The runner downloads a pinned Venom into `venom/bin/` on first use, so nothing
is installed globally and changing the version is a commit.

## What they test, and what they cannot

The suites cover **the MCP surface only**: that the server starts, completes the
handshake, declares what it must declare, and exposes the right tools with the
right parameters.

Two of those assertions matter more than the rest, because the failures they
catch are invisible — nothing errors, agents still connect, sends still report
success, and only delivery into the conversation stops:

- that the `claude/channel` capability is still declared;
- that the protocol revision which breaks channels is not negotiated.

They deliberately do **not** test that a message reaches another agent. That
needs a live Claude Code session started with gocc2cc named as a channel, which
no test harness can arrange. Routing and delivery are covered by the Go tests in
`internal/broker/`.

Because the shim stays alive with no broker running, these suites need nothing
else started — just the binary.

## Layout

```
test/
├── 01-mcp-surface.yml      one file per flow; the number orders execution
├── environments/local.yml  values the suites reference — nothing env-specific in the suites
├── run-venom.sh
└── venom/{bin,log}/        pinned runner, and logs wiped on each run
```

## Conventions

- One testcase with many ordered steps, rather than many testcases. State moves
  forward through step `vars`.
- Only the `http` and `exec` executors. Anything awkward is done in `exec`.
- MCP is a session, so a step cannot send a bare request: each pipes the whole
  sequence — initialize, initialized, then the request under test — into one
  invocation, since Venom cannot hold a process open across steps.
- Assert negatives too. `ShouldNotContainSubstring 2026-07-28` is the test that
  catches a failure with no other symptom.

## When a suite fails

Logs land in `venom/log/`, wiped at the start of each run. `traces.log` collects
the captured output from each step in readable form, which is usually where the
answer is.
