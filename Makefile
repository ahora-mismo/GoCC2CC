BINARY      := gocc2cc
BUILD_DIR   := build
ROOT        := $(HOME)/.gocc2cc
# Claude Code caches a plugin under its version, so a fresh stamp each install
# guarantees the cache holds what was just built rather than an older copy.
VERSION     := $(shell date +%Y%m%d_%H%M)
MARKETPLACE := $(ROOT)/marketplace
PLUGIN      := $(MARKETPLACE)/gocc2cc

.DEFAULT_GOAL := help

## help: list the available targets
help:
	@echo "gocc2cc"
	@echo
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  make /' | sed 's/: /\t/' | expand -t 22
	@echo

## build: compile the binary into build/
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/gocc2cc
	@echo "built $(BUILD_DIR)/$(BINARY)"

## install: build, then set up ~/.gocc2cc and register the plugin
install: build
	@mkdir -p $(ROOT)/bin $(PLUGIN)/.claude-plugin $(PLUGIN)/skills/gocc2cc $(PLUGIN)/hooks $(MARKETPLACE)/.claude-plugin
	@chmod 700 $(ROOT)
	@# Replace rather than overwrite: writing over a binary in place invalidates
	@# its code signature on macOS, and the kernel then kills it on exec.
	@rm -f $(ROOT)/bin/$(BINARY)
	@cp $(BUILD_DIR)/$(BINARY) $(ROOT)/bin/$(BINARY)
	@sed 's|__GOCC2CC_VERSION__|$(VERSION)|' plugin/.claude-plugin/plugin.json > $(PLUGIN)/.claude-plugin/plugin.json
	@cp plugin/skills/gocc2cc/SKILL.md $(PLUGIN)/skills/gocc2cc/SKILL.md
	@cp plugin/hooks/hooks.json $(PLUGIN)/hooks/hooks.json
	@cp plugin/hooks/session-start.sh $(PLUGIN)/hooks/session-start.sh
	@chmod +x $(PLUGIN)/hooks/session-start.sh
	@cp marketplace/.claude-plugin/marketplace.json $(MARKETPLACE)/.claude-plugin/marketplace.json
	@sed 's|__GOCC2CC_BIN__|$(ROOT)/bin/$(BINARY)|' plugin/.mcp.json.tmpl > $(PLUGIN)/.mcp.json
	@echo "installed into $(ROOT)"
	@$(MAKE) --no-print-directory register
	@$(MAKE) --no-print-directory verify

# register points Claude Code at the local marketplace and installs the plugin
# from it.
#
# The uninstall is not optional. `claude plugin install` does nothing when the
# plugin is already installed — it does not even read the manifest — so without
# removing it first, Claude Code keeps serving whatever it cached the first time
# and every later change is invisible.
register:
	@claude plugin marketplace add $(MARKETPLACE) >/dev/null 2>&1 \
		|| claude plugin marketplace update gocc2cc-local >/dev/null 2>&1 \
		|| echo "  note: could not register the marketplace automatically — see README.md"
	@claude plugin uninstall gocc2cc@gocc2cc-local >/dev/null 2>&1 || true
	@claude plugin marketplace update gocc2cc-local >/dev/null 2>&1 || true
	@claude plugin install gocc2cc@gocc2cc-local >/dev/null 2>&1 \
		|| echo "  note: could not install the plugin automatically — see README.md"
	@echo "  plugin version $(VERSION)"

# verify checks what it can. It cannot confirm that messages actually reach
# another agent: that needs a Claude Code session started with gocc2cc named as
# a channel, which a Makefile cannot arrange.
verify:
	@$(ROOT)/bin/$(BINARY) --help >/dev/null 2>&1 \
		&& echo "  binary runs" \
		|| (echo "  the installed binary does not run" && exit 1)
	@echo
	@echo "Start Claude Code with gocc2cc loaded as a channel:"
	@echo
	@echo "    claude --dangerously-load-development-channels plugin:gocc2cc@gocc2cc-local"
	@echo
	@echo "Then run 'gocc2cc' in another terminal to watch the sessions."

## uninstall: remove ~/.gocc2cc and unregister the plugin
uninstall:
	@claude plugin marketplace remove gocc2cc-local >/dev/null 2>&1 || true
	@rm -rf $(ROOT)
	@echo "removed $(ROOT)"

## test: run the unit tests
test:
	go test ./...

## test-integration: run the Venom suites against the built binary
test-integration: build
	bash test/run-venom.sh local

## fmt: format the source
fmt:
	gofmt -w .

## clean: remove build output
clean:
	rm -rf $(BUILD_DIR)

.PHONY: help build install register verify uninstall test test-integration fmt clean
