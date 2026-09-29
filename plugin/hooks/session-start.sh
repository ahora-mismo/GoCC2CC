#!/usr/bin/env bash
# Runs when a Claude Code session starts, and again when one is cleared,
# compacted, resumed or forked.
#
# All it does is hand the payload to the binary. Keeping the logic in Go rather
# than in shell means no `jq` prerequisite, and somewhere the parsing can be
# tested.
set -euo pipefail
exec "${HOME}/.gocc2cc/bin/gocc2cc" hook session-start
