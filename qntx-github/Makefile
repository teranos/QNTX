.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build GitHub plugin binary
	@echo "Building qntx-github-plugin..."
	@go build -o bin/qntx-github-plugin ./cmd/qntx-github-plugin
	@echo "✓ bin/qntx-github-plugin"

install: build ## Install GitHub plugin to ~/.qntx/plugins/
	@echo "Installing qntx-github-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-github-plugin $(PREFIX)/plugins/qntx-github-plugin
	@chmod +x $(PREFIX)/plugins/qntx-github-plugin
	@echo "✓ qntx-github-plugin → $(PREFIX)/plugins/qntx-github-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/
