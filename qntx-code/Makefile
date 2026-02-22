.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build code plugin binary
	@echo "Building qntx-code-plugin..."
	@go build -o bin/qntx-code-plugin ./cmd/qntx-code-plugin
	@echo "✓ bin/qntx-code-plugin"

install: build ## Install code plugin to ~/.qntx/plugins/
	@echo "Installing qntx-code-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-code-plugin $(PREFIX)/plugins/qntx-code-plugin
	@chmod +x $(PREFIX)/plugins/qntx-code-plugin
	@echo "✓ qntx-code-plugin → $(PREFIX)/plugins/qntx-code-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/
