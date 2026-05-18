.PHONY: build install clean test check

PREFIX ?= $(HOME)/.qntx
QNTX_ROOT ?= $(HOME)/SBVH/teranos/QNTX
PLUGIN_NAME := python
BINARY_NAME := qntx-python-plugin

# Auto-bump patch version before every build.
define bump-version
	@CURRENT=$$(sed -n 's/^version = "\(.*\)"/\1/p' Cargo.toml); \
	 MAJOR=$$(echo $$CURRENT | cut -d. -f1); \
	 MINOR=$$(echo $$CURRENT | cut -d. -f2); \
	 PATCH=$$(echo $$CURRENT | cut -d. -f3); \
	 NEXT="$$MAJOR.$$MINOR.$$((PATCH + 1))"; \
	 sed -i '' "s/^version = \"$$CURRENT\"/version = \"$$NEXT\"/" Cargo.toml; \
	 echo "  ⬆ $$CURRENT → $$NEXT"
endef

# Restart — tell running QNTX to hot-restart, then poll until version confirmed.
define restart-plugin
	@TOML_PORT=$$(grep -E '^port\s*=' $(QNTX_ROOT)/am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	 PORT=$${BACKEND_PORT:-$${TOML_PORT:-8770}}; \
	 EXPECTED=$$(sed -n 's/^version = "\(.*\)"/\1/p' Cargo.toml); \
	 curl -sf -X POST http://127.0.0.1:$$PORT/api/plugins/$(PLUGIN_NAME)/restart > /dev/null 2>&1 || { \
	   echo "  ⊘ NOT LOADED — QNTX server is not running, binary is built but not active"; \
	   echo "    → run: make dev"; exit 0; }; \
	 echo "  ↻ RESTARTING $(PLUGIN_NAME) (expecting $$EXPECTED)..."; \
	 for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57 58 59 60; do \
	   STATE=$$(curl -sf http://127.0.0.1:$$PORT/api/plugins 2>/dev/null \
	     | python3 -c "import sys,json; plugins=json.load(sys.stdin).get('plugins',[]); p=[x for x in plugins if x.get('name')=='$(PLUGIN_NAME)']; print(p[0].get('state','') if p else '')" 2>/dev/null); \
	   if [ "$$STATE" = "running" ]; then \
	     VERSION=$$(curl -sf http://127.0.0.1:$$PORT/api/plugins 2>/dev/null \
	       | python3 -c "import sys,json; plugins=json.load(sys.stdin).get('plugins',[]); p=[x for x in plugins if x.get('name')=='$(PLUGIN_NAME)']; print(p[0].get('version','') if p else '')" 2>/dev/null); \
	     if [ -z "$$VERSION" ]; then :; \
	     elif [ "$$VERSION" = "$$EXPECTED" ]; then \
	       echo "  ✓ LOADED — $(PLUGIN_NAME) $$VERSION live at http://127.0.0.1:$$PORT"; exit 0; \
	     else \
	       echo "  ✗ WRONG VERSION — expected $$EXPECTED, got $$VERSION"; exit 1; \
	     fi; \
	   fi; \
	   sleep 1; \
	 done; \
	 echo "  ✗ $(PLUGIN_NAME) did not reach running state within 60s (expected $$EXPECTED)"
endef

build: ## Build python plugin via Nix
	$(call bump-version)
	@echo "Building $(BINARY_NAME) via Nix..."
	@cd $(QNTX_ROOT) && nix build ./qntx-plugins/qntx-python#qntx-python-plugin
	@mkdir -p bin
	@cp -L $(QNTX_ROOT)/result/bin/$(BINARY_NAME) bin/
	@chmod +x bin/$(BINARY_NAME)
	@echo "✓ bin/$(BINARY_NAME)"

test: ## Run Rust tests
	@echo "Running $(PLUGIN_NAME) tests..."
	@cd $(QNTX_ROOT) && cargo test -p qntx-python-plugin
	@echo "✓ $(PLUGIN_NAME) tests passed"

check: ## Run clippy
	@cd $(QNTX_ROOT) && cargo clippy -p qntx-python-plugin -- -D warnings

install: build ## Build, install, and restart python plugin
	@echo "Installing $(BINARY_NAME) to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/$(BINARY_NAME) $(PREFIX)/plugins/$(BINARY_NAME)
	@chmod +x $(PREFIX)/plugins/$(BINARY_NAME)
	@echo "✓ $(BINARY_NAME) → $(PREFIX)/plugins/$(BINARY_NAME)"
	$(call restart-plugin)

clean: ## Clean build artifacts
	@rm -rf bin/ target/
