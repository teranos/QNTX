.PHONY: build install clean test

PREFIX ?= $(HOME)/.qntx
DC ?= ldc2
DUB ?= dub

build: ## Build ix-otlp plugin binary
	@echo "Building qntx-ix-otlp-plugin (D/LDC)..."
	@$(DUB) build --compiler=$(DC) --config=application
	@echo "✓ bin/qntx-ix-otlp-plugin"

test: ## Run D unit tests
	@echo "Running ix-otlp unit tests..."
	@$(DUB) test --compiler=$(DC)
	@echo "✓ ix-otlp tests passed"

install: build ## Install ix-otlp plugin to ~/.qntx/plugins/
	@echo "Installing qntx-ix-otlp-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-ix-otlp-plugin $(PREFIX)/plugins/qntx-ix-otlp-plugin
	@chmod +x $(PREFIX)/plugins/qntx-ix-otlp-plugin
	@echo "✓ qntx-ix-otlp-plugin → $(PREFIX)/plugins/qntx-ix-otlp-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/ .dub/
