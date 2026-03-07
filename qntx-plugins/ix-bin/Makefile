.PHONY: build install clean test

PREFIX ?= $(HOME)/.qntx
DC ?= ldc2
DUB ?= dub

build: ## Build ix-bin plugin binary
	@echo "Building qntx-ix-bin-plugin (D/LDC)..."
	@$(DUB) build --compiler=$(DC) --config=application
	@echo "✓ bin/qntx-ix-bin-plugin"

test: ## Run D unit tests
	@echo "Running ix-bin unit tests..."
	@$(DUB) test --compiler=$(DC)
	@echo "✓ ix-bin tests passed"

install: build ## Install ix-bin plugin to ~/.qntx/plugins/
	@echo "Installing qntx-ix-bin-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-ix-bin-plugin $(PREFIX)/plugins/qntx-ix-bin-plugin
	@chmod +x $(PREFIX)/plugins/qntx-ix-bin-plugin
	@echo "✓ qntx-ix-bin-plugin → $(PREFIX)/plugins/qntx-ix-bin-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/ .dub/
