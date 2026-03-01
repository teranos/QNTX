.PHONY: build build-module install clean

PREFIX ?= $(HOME)/.qntx

build-module: ## Compile TypeScript glyph module to JS for go:embed
	@bun build web/ix-glyph-module.ts --outfile web/ix-glyph-module.js --target browser

build: build-module ## Build ix-json plugin binary
	@echo "Building qntx-ix-json-plugin..."
	@go build -o bin/qntx-ix-json-plugin ./cmd/qntx-ix-json-plugin
	@echo "✓ bin/qntx-ix-json-plugin"

install: build ## Install ix-json plugin to ~/.qntx/plugins/
	@echo "Installing qntx-ix-json-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-ix-json-plugin $(PREFIX)/plugins/qntx-ix-json-plugin
	@chmod +x $(PREFIX)/plugins/qntx-ix-json-plugin
	@echo "✓ qntx-ix-json-plugin → $(PREFIX)/plugins/qntx-ix-json-plugin"

clean: ## Clean build artifacts
	@rm -rf bin/
