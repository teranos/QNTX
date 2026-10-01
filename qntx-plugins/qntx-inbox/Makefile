.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build inbox plugin binary
	@echo "Building qntx-inbox-plugin..."
	@go build -o bin/qntx-inbox-plugin ./cmd/qntx-inbox-plugin
	@echo "✓ bin/qntx-inbox-plugin"

install: build ## Install inbox plugin to ~/.qntx/plugins/
	@echo "Installing qntx-inbox-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-inbox-plugin $(PREFIX)/plugins/qntx-inbox-plugin
	@chmod +x $(PREFIX)/plugins/qntx-inbox-plugin
	@echo "✓ qntx-inbox-plugin → $(PREFIX)/plugins/qntx-inbox-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/
