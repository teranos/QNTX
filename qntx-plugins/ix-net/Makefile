.PHONY: build install clean test

PREFIX ?= $(HOME)/.qntx
DC ?= ldc2
DUB ?= dub

build: ## Build ix-net plugin binary
	@echo "Building qntx-ix-net-plugin (D/LDC)..."
	@$(DUB) build --compiler=$(DC) --config=application
	@echo "✓ bin/qntx-ix-net-plugin"

test: ## Run D unit tests
	@echo "Running ix-net unit tests..."
	@$(DUB) test --compiler=$(DC)
	@echo "✓ ix-net tests passed"

install: build ## Install ix-net plugin to ~/.qntx/plugins/
	@echo "Installing qntx-ix-net-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-ix-net-plugin $(PREFIX)/plugins/qntx-ix-net-plugin
	@chmod +x $(PREFIX)/plugins/qntx-ix-net-plugin
	@mkdir -p $(PREFIX)/certs
	@if [ -f certs/leaf.pem ] && [ -f certs/leaf.key ]; then \
		cp certs/leaf.pem certs/leaf.key certs/ca.pem $(PREFIX)/certs/; \
		echo "✓ certs → $(PREFIX)/certs/"; \
	else \
		echo "⚠ certs not found — run: cd certs && fish anthropic.fish"; \
	fi
	@echo "✓ qntx-ix-net-plugin → $(PREFIX)/plugins/qntx-ix-net-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/ .dub/
