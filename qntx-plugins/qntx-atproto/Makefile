.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build AT Protocol plugin binary
	@echo "Building qntx-atproto-plugin..."
	@go build -o bin/qntx-atproto-plugin ./cmd/qntx-atproto-plugin
	@echo "✓ bin/qntx-atproto-plugin"

install: build ## Install AT Protocol plugin to ~/.qntx/plugins/
	@echo "Installing qntx-atproto-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-atproto-plugin $(PREFIX)/plugins/qntx-atproto-plugin
	@chmod +x $(PREFIX)/plugins/qntx-atproto-plugin
	@echo "✓ qntx-atproto-plugin → $(PREFIX)/plugins/qntx-atproto-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/
