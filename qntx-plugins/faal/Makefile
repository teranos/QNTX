.PHONY: build install clean test

PREFIX ?= $(HOME)/.qntx
DC ?= ldc2
DUB ?= dub

build: ## Build faal plugin binary
	@echo "Building qntx-faal-plugin (D/LDC)..."
	@$(DUB) build --compiler=$(DC) --config=application
	@echo "✓ bin/qntx-faal-plugin"

test: ## Run D unit tests
	@echo "Running faal unit tests..."
	@$(DUB) test --compiler=$(DC)
	@echo "✓ faal tests passed"

install: build ## Install faal plugin to ~/.qntx/plugins/
	@echo "Installing qntx-faal-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-faal-plugin $(PREFIX)/plugins/qntx-faal-plugin
	@chmod +x $(PREFIX)/plugins/qntx-faal-plugin
	@echo "✓ qntx-faal-plugin → $(PREFIX)/plugins/qntx-faal-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/ .dub/
