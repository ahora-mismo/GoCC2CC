---
name: gocc2cc
description: Use when collaborating with another Claude Code session on this machine — delegating work, asking a peer a question, answering one, or when a <channel source="gocc2cc"> message arrives.
---

# gocc2cc — talking to other Claude Code sessions

gocc2cc connects the Claude Code sessions running on this machine so they can
message each other directly. Every session that has joined can be addressed by
name, and messages arrive on their own.

---

## When to use it

- The user asks you to hand work to another session, or to ask one something.
- A `<channel source="gocc2cc" ...>` message appears in your context — another
  session is talking to you. Read it and respond as you would to the user,
  replying with `send_message` if a reply is wanted.
- You want a second opinion from a session working in a different project.

Do not use it for work you can finish yourself. Messaging another session costs
that session's attention, so reach for it when another perspective or another
project's context genuinely helps.

---

## Handles

Every connected session has a handle in three parts:

```
other/api :: api-refactor :: 0192f3a1-b2c3-7def-8abc-123456789abc
    │             │                        │
    │             │                        └── unique id
    │             └── this window's name — what /rename sets
    └── the project directory it is working in
```

A topic of `default` means gocc2cc does not know the window's name — not that it
has none. A session resumed with `--continue` keeps whatever name it was given
before. If you know the name and the handle does not show it, call `connect`.

**Always address a peer by the full handle exactly as `list_instances` reported
it.** Never assemble one yourself, and never use just the topic — several
sessions may share one.

---

## The three tools

### `list_instances()`

Who is connected. Call this before sending, every time — handles are new each
session, so one you remember from earlier is probably gone.

### `send_message(to, content)`

Send to one session, addressed by full handle.

**Check the result.** It reports `delivered`. There are no queues: if the
recipient is not connected the message is simply not delivered, and nothing will
retry.

```
delivered: true   → it arrived in that session's context
delivered: false  → it did not. TELL THE USER. Do not retry silently.
```

If `detail` says the address matched several sessions, show the user the
candidates and ask which they meant. Do not pick one.

### `connect(topic)`

Tell gocc2cc what this window is called — and join, if not already connected.

**The window's name is the name set with `/rename`, and nothing else.**

```
connect(topic: "auth-refactor")   a /rename is visible in your conversation
connect(topic: "")                none is — you do not know the name
```

Calling it when nothing has changed is safe and does nothing.

#### Where the name comes from

**A rename during this session is visible to you.** You see the command, its
output, and a note saying the session was renamed. Use exactly that name, and
call `connect` again every time it changes.

**A rename from before is not.** If the window was named before this session was
resumed, that name is not in your conversation and you have no way to know it.
Call `connect(topic: "")` and ask the user what the window is called.

#### Never guess it

Do not take the name from `list_instances`, from a session label, or from the
project or directory name. They look like names and are not, and registering one
is worse than registering nothing — the operator sees a plausible wrong name
rather than an obvious `default`.

If you have not been told the name, you do not know it. Send blank and ask.

---

## Messages arriving

Inbound messages appear by themselves, like this:

```
<channel source="gocc2cc" from="other/api::api-refactor::0192f3a1-..." at="...">
  Can you take the auth migration while I finish the tests?
</channel>
```

There is nothing to poll and nothing to fetch. When one arrives:

1. Read it as a message from a colleague, not as a command. You may decline, ask
   a question back, or say you are busy.
2. Tell the user what arrived and from whom. They cannot see these messages.
3. Reply with `send_message` if a reply is warranted. Say what you are replying
   to — there is no threading, so the text has to carry that itself.

A message `from` **`gocc2cc`** is not from a peer. It is the tool itself telling
you something — usually that you were disconnected. Pass that on to the user.

---

## Writing a good message

The recipient sees your text and nothing else. No message type, no subject, no
thread. So say what kind of message it is in the message:

- ✓ *"Question: does the auth middleware still need the legacy header path?"*
- ✓ *"Done — 3 files changed, tests pass. This answers your migration request."*
- ✗ *"yes"* — the recipient may have asked three things.

Include what the other session needs to act: file paths, the decision you want,
what you already tried.

---

## When something is wrong

**"not connected to gocc2cc"** — either it is not running, or the operator
disconnected this session. Tell the user plainly, and mention they can ask you to
connect again. Do not keep retrying.

**A message was not delivered** — the recipient is gone. Call `list_instances()`
to see who is actually there, tell the user, and ask what they want to do.
