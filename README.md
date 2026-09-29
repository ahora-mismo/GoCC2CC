# gocc2cc

gocc2cc lets Claude Code sessions on the same machine send messages to each
other.

I usually have several projects open at once, each in its own repo with its own
Claude Code session. Each session knows its own project well and knows nothing
about the others, so I kept ending up as the messenger: copying code from one
window into another, explaining how some service works, pasting answers back.
With gocc2cc the agents just ask each other. The one working on the mobile app
asks the one working on the auth service what the token looks like, and gets
the answer from an agent that already has that code open.

```
you, in the mobile-app session:
  check with the auth service agent what the token carries and how refresh works

claude:
  Asked work/auth-service::sso-rework::…b71e00. Access tokens carry sub, org_id
  and roles and expire after 15 minutes. Refresh is POST /token/refresh with a
  rotating refresh token, and a reused one gets rejected. I'll build the client
  around that.
```

It works in both directions, so the auth service agent can also ask the mobile
agent which claims it reads before removing one.

## Why bother

The alternative is letting one agent read the other project's source. That
works, but it has to find the right files, follow the calls and load a lot of
code just to learn a few facts, and all of that stays in its context for the
rest of the session.

Asking the agent that owns the code is cheaper. That agent has usually been
working in the area already and knows the answer, so nothing gets read twice.
If it does have to look, it knows where. Each session's context stays about its
own project, so it doesn't mix up conventions from two codebases, it hits
compaction later, and it loses less when it does. You use fewer tokens, and you
stop relaying things between windows by hand.

## What it is

One Go binary that does three jobs:

- `gocc2cc` starts a small broker and a terminal UI where you can see who is
  connected and what's being sent. It listens on a Unix socket in `~/.gocc2cc/`
  and keeps everything in memory. Nothing goes to disk, nothing listens on the
  network, and there's nothing to configure.
- `gocc2cc mcp` is the MCP server Claude Code starts for each session (you
  never run it yourself). It gives the agent its tools, connects to the broker
  and hands incoming messages to Claude.
- `gocc2cc hook` runs from a `SessionStart` hook. It tells each new session
  that gocc2cc is there and asks for the window's name. It also runs after
  `/clear`, compaction, resume and fork.

It comes with a Claude Code plugin that wires this up, plus a skill that tells
the agent when a message is worth sending, how to address a peer, and that it
has to tell you if a send fails.

The agent gets three tools:

| Tool | |
|---|---|
| `list_instances` | who is connected right now, with their handles |
| `send_message` | send to one handle; says whether it was delivered |
| `connect` | join the broker and report this window's name |

## Channels (read this or nothing will arrive)

Normally an MCP server can only reply when Claude calls one of its tools. It has
no way to reach Claude by itself, so a message from another session would just
sit there until Claude thought to check.

Claude Code has a feature called channels that solves this. A server that
declares the `claude/channel` capability can send
`notifications/claude/channel` whenever it wants, and Claude Code drops it into
the conversation like this:

```
<channel source="gocc2cc" from="work/auth-service::sso-rework::0192…b71e00" at="2026-09-11T09:41:18Z">
Access tokens carry sub, org_id and roles and expire after 15 minutes. …
</channel>
```

Claude picks it up on its next turn. That's why gocc2cc has no inbox tool and
never polls.

Channels are still a research preview, and they're off unless you turn them on
when you start Claude Code:

```bash
claude --dangerously-load-development-channels plugin:gocc2cc@gocc2cc-local
```

Installing the plugin isn't enough. Without that flag the session still
connects and can still send, but messages to it never show up, and nothing
tells you why. I use an alias:

```bash
alias claudex='claude --dangerously-load-development-channels plugin:gocc2cc@gocc2cc-local'
```

You also need Claude Code 2.1.80 or newer, logged in with a claude.ai account
or the Anthropic Console. Channels don't work through Bedrock, Vertex or
Foundry. On Team and Enterprise plans an org owner has to enable them first.

## JetBrains IDEs

This works in all the IntelliJ-based IDEs (IntelliJ IDEA, GoLand, PyCharm,
WebStorm, PhpStorm, Rider, CLion, RubyMine, DataGrip, Android Studio). Claude
Code in an IDE is the same `claude` binary, so it picks up the same plugin, hook
and broker. Start it in the IDE's terminal with the flag above. Every IDE window
and every terminal on the machine ends up on the same broker, so a session in
GoLand can talk to one in WebStorm or in iTerm like any two terminals would.

## Install

You need Go 1.26+, the Claude Code CLI on your `PATH`, and macOS or Linux (it
uses a Unix socket).

```bash
git clone <this repo>
cd gocc2cc
make install
```

`make install` builds the binary, copies everything into `~/.gocc2cc/` and
registers the plugin through a local marketplace. Nothing is downloaded or
published. The installed copy doesn't depend on the checkout, so you can move or
delete the repo afterwards. Add `~/.gocc2cc/bin` to your `PATH` or call the
binary by its full path.

If the plugin didn't get registered, do it by hand:

```bash
claude plugin marketplace add ~/.gocc2cc/marketplace
claude plugin install gocc2cc@gocc2cc-local
```

`make uninstall` removes everything.

## Usage

Run `gocc2cc` in its own terminal and leave it open. Then start Claude Code
with the channels flag in as many terminals or IDE windows as you want, and
give each window a name with `/rename`.

After that you just ask, for example "ask the agent on the billing service
which events it sends when an invoice is paid" or "tell the reporting agent the
new field is live". The agent checks who's connected, sends the message and
tells you what came back.

A session that started before the broker was running, or that got
disconnected, won't reconnect by itself. Ask it to connect to gocc2cc.

Keys in the UI:

| Key | |
|---|---|
| `1` / `2` | connections / messages tab |
| `↑` `↓` | move |
| `←` `→` | jump to top / bottom; on messages `←` also resumes following |
| `enter` | open a message |
| `k` `k` | disconnect the selected session (press twice) |
| `q` `q` | quit; this disconnects everyone, so it asks too |
| `ctrl+c` | quit right away |

### Handles

Sessions are addressed by handles like
`work/webapp::docs-fix::0192f3a1-b2c3-7def-8abc-123456789abc`, which is
`app::topic::id`. The app is the last two folders of the working directory. The
topic is whatever you named the window with `/rename`, or `default` until you do
(the agent calls `connect` again after a rename). The id is a UUIDv7 that's new
on every registration, and the UI shows its last six characters. Agents always
take handles from `list_instances` rather than building them. When a session
reconnects it gets a new id, so the others have to look it up again.

## Behaviour

- Messages aren't queued. If the recipient isn't connected, the send fails and
  the sending agent tells you. There are no retries.
- The connections list is whoever has an open connection right now. When a
  session closes, it's gone from the list immediately.
- Pressing `k` tells the session it was removed. It stays disconnected until
  you ask it to connect again.
- The messages tab holds the last 100 messages and drops anything older than
  48 hours. It's there to watch recent traffic and doesn't write anything to
  disk.
- When the broker quits, every connected session is told.

## Security

The only thing protecting the socket is that `~/.gocc2cc/` is readable by you
alone. I didn't add a key, because anything that can open the socket can most
likely read a key file too. So any process running as your user can connect and
put text into a running agent's context. Worth remembering if your agents touch
anything sensitive.

## MCP protocol revision 2026-07-28

gocc2cc refuses MCP protocol revision 2026-07-28. That revision can't carry
channel messages, and if a server negotiates it, Claude Code quietly doesn't
register it as a channel. Everything looks fine, sessions connect and sends
report success, but nothing reaches the conversation. Refusing the revision
keeps push working however the machine is set up. It's a workaround; once that
revision supports channels, the check in `internal/mcp/transport.go` should be
revisited.

## Development

```bash
make                   # list targets
make build             # build into build/
make test              # unit tests
make test-integration  # Venom suites against the built binary
make install           # the installed copy is what actually runs
```

- `cmd/gocc2cc`: entry point and the three modes
- `internal/core`: handles, socket frames, the `~/.gocc2cc` layout, the session hook
- `internal/broker`: connections, routing, the message log
- `internal/mcp`: the MCP server, its tools and channel push
- `internal/ui`: the terminal UI (rendering only)
- `plugin/`: what Claude Code loads, see `plugin/README.md`
- `test/`: Venom integration tests, see `test/README.md`

## Thanks

gocc2cc started as a local, single-binary take on
[cc2cc](https://github.com/paulrobello/cc2cc), which connects Claude Code
instances across a LAN. Thanks for the idea.
