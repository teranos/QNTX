.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx
WORKSPACE_ROOT := $(shell cd ../.. && pwd)

# Restart — tell running QNTX to hot-restart meili. Silent no-op if QNTX isn't running.
define restart-plugin
	@TOML_PORT=$$(grep -E '^port\s*=' ../../am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	 PORT=$${BACKEND_PORT:-$${TOML_PORT:-877}}; \
	 curl -sf -X POST http://127.0.0.1:$$PORT/api/plugins/meili/restart > /dev/null 2>&1 \
		&& echo "  ↻ LOADED — meili restarted, live at http://127.0.0.1:$$PORT" \
		|| (echo "  ⊘ NOT LOADED — QNTX server is not running, binary is built but not active" && echo "    → run: make dev")
endef

install: ## Build, install, and restart qntx-meili plugin
	@echo "Building qntx-meili..."
	@cargo build --release -p qntx-meili
	@mkdir -p $(PREFIX)/plugins
	@cp $(WORKSPACE_ROOT)/target/release/qntx-meili $(PREFIX)/plugins/qntx-meili
	@chmod +x $(PREFIX)/plugins/qntx-meili
	@codesign -s - --force $(PREFIX)/plugins/qntx-meili
	@echo "✓ BUILD OK — binary installed to $(PREFIX)/plugins/qntx-meili"
	$(call restart-plugin)

clean: ## Clean build artifacts
	@cargo clean
