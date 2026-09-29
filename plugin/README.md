# plugin/

The files Claude Code needs in order to load gocc2cc. They do nothing on their
own — they are copied elsewhere by `make install` and only mean anything to
Claude Code.

## What each file is

| File | What it does |
|---|---|
| `.claude-plugin/plugin.json` | The plugin manifest: name, version, and a pointer to the MCP config. |
| `.mcp.json.tmpl` | **A template.** Tells Claude Code how to spawn the shim. `__GOCC2CC_BIN__` is replaced with the real path at install time. |
| `skills/gocc2cc/SKILL.md` | What the agent reads: when to message another session, how handles work, and that an undelivered message must be reported to the user. |

`../marketplace/` holds the other half — a marketplace manifest listing this one
plugin. A marketplace is just a directory Claude Code has been told to look in;
nothing is published anywhere.

## Where they end up

`make install` copies them into `~/.gocc2cc/`, substituting the binary path:

```
~/.gocc2cc/
├── bin/gocc2cc
└── marketplace/
    ├── .claude-plugin/marketplace.json    ← from ../marketplace/
    └── gocc2cc/
        ├── .claude-plugin/plugin.json
        ├── .mcp.json                      ← generated from .mcp.json.tmpl
        └── skills/gocc2cc/SKILL.md
```

Nothing installed references this checkout, so the repository can be moved or
deleted afterwards.

## Changing them

Edit here, then `make install` again.

Claude Code caches plugins by version, so if a change does not appear to take
effect, restart Claude Code. Nothing bumps the version automatically — it is a
release marker, not a cache key we manage.

The skill is the part most worth getting right. It is the only thing telling an
agent *when* messaging another session is worthwhile, and that a failed send
must be mentioned to the user rather than silently retried.
