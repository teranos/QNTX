.PHONY: build install clean test

PREFIX ?= $(HOME)/.qntx
DC ?= ldc2
DUB ?= dub

build: ## Build infer plugin binary
	@echo "Building qntx-infer-plugin (D/LDC)..."
	@$(DUB) build --compiler=$(DC) --config=application
	@echo "✓ bin/qntx-infer-plugin"

test: ## Run D unit tests
	@echo "Running infer unit tests..."
	@$(DUB) test --compiler=$(DC)
	@echo "✓ infer tests passed"

install: build ## Install infer plugin to ~/.qntx/plugins/
	@echo "Installing qntx-infer-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-infer-plugin $(PREFIX)/plugins/qntx-infer-plugin
	@chmod +x $(PREFIX)/plugins/qntx-infer-plugin
	@echo "✓ qntx-infer-plugin → $(PREFIX)/plugins/qntx-infer-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/ .dub/
