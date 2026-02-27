.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build OpenRouter plugin binary
	@echo "Building qntx-openrouter-plugin..."
	@go build -o bin/qntx-openrouter-plugin ./cmd/qntx-openrouter-plugin
	@echo "✓ bin/qntx-openrouter-plugin"

install: build ## Install OpenRouter plugin to ~/.qntx/plugins/
	@echo "Installing qntx-openrouter-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-openrouter-plugin $(PREFIX)/plugins/qntx-openrouter-plugin
	@chmod +x $(PREFIX)/plugins/qntx-openrouter-plugin
	@echo "✓ qntx-openrouter-plugin → $(PREFIX)/plugins/qntx-openrouter-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/
