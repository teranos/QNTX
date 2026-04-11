.PHONY: install clean

PREFIX ?= $(HOME)/.qntx
BUILD_DIR = build

# Restart — tell running QNTX to hot-restart gaze. Silent no-op if QNTX isn't running.
define restart-plugin
	@TOML_PORT=$$(grep -E '^port\s*=' ../../am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	 PORT=$${BACKEND_PORT:-$${TOML_PORT:-877}}; \
	 curl -sf -X POST http://127.0.0.1:$$PORT/api/plugins/gaze/restart > /dev/null 2>&1 \
		&& echo "  ↻ LOADED — gaze restarted, live at http://127.0.0.1:$$PORT" \
		|| (echo "  ⊘ NOT LOADED — QNTX server is not running, binary is built but not active" && echo "    → run: make dev")
endef

install: ## Build, install, and restart gaze plugin
	@echo "Building qntx-gaze-plugin..."
	@mkdir -p $(BUILD_DIR)
	@cd $(BUILD_DIR) && cmake .. -DCMAKE_BUILD_TYPE=Release
	@cd $(BUILD_DIR) && cmake --build . --parallel 5
	@mkdir -p $(PREFIX)/plugins
	@cp $(BUILD_DIR)/qntx-gaze-plugin $(PREFIX)/plugins/qntx-gaze-plugin
	@chmod +x $(PREFIX)/plugins/qntx-gaze-plugin
	@echo "✓ BUILD OK — binary installed to $(PREFIX)/plugins/qntx-gaze-plugin"
	$(call restart-plugin)

clean: ## Clean build artifacts
	@rm -rf $(BUILD_DIR)
	@echo "✓ Clean"
