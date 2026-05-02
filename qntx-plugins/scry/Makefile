.PHONY: install clean

PREFIX ?= $(HOME)/.qntx
BUILD_DIR = build

# Restart — tell running QNTX to hot-restart scry. Silent no-op if QNTX isn't running.
define restart-plugin
	@TOML_PORT=$$(grep -E '^port\s*=' ../../am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	 PORT=$${BACKEND_PORT:-$${TOML_PORT:-8770}}; \
	 curl -sf -X POST http://127.0.0.1:$$PORT/api/plugins/scry/restart > /dev/null 2>&1 \
		&& echo "  ↻ scry restarted — live at http://127.0.0.1:$$PORT" \
		|| echo "  ⊘ QNTX not running — restart make dev to pick up new binary"
endef

install: ## Build, install, and restart scry plugin
	@echo "Building qntx-scry-plugin..."
	@mkdir -p $(BUILD_DIR)
	@cd $(BUILD_DIR) && cmake .. -DCMAKE_BUILD_TYPE=Release
	@cd $(BUILD_DIR) && cmake --build . --parallel 5
	@mkdir -p $(PREFIX)/plugins
	@cp $(BUILD_DIR)/qntx-scry-plugin $(PREFIX)/plugins/qntx-scry-plugin
	@chmod +x $(PREFIX)/plugins/qntx-scry-plugin
	@if [ -f $(BUILD_DIR)/default.metallib ]; then \
		cp $(BUILD_DIR)/default.metallib $(PREFIX)/plugins/default.metallib; \
		echo "✓ default.metallib → $(PREFIX)/plugins/default.metallib"; \
	fi
	@echo "✓ qntx-scry-plugin → $(PREFIX)/plugins/qntx-scry-plugin"
	$(call restart-plugin)

clean: ## Clean build artifacts
	@rm -rf $(BUILD_DIR)
	@echo "✓ Clean"
