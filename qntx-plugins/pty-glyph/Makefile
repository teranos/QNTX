.PHONY: build build-module install clean

PREFIX ?= $(HOME)/.qntx
WORKSPACE_ROOT := $(shell cd ../.. && pwd)

build-module: ## Compile TypeScript glyph module to JS for include_str!
	@cd web && bun install --frozen-lockfile
	@bun build web/pty-glyph-module.ts --outfile web/pty-glyph-module.js --target browser
	@cp web/node_modules/@xterm/xterm/css/xterm.css web/xterm.css

build: build-module ## Build pty-glyph plugin binary
	@echo "Building qntx-pty-glyph..."
	@cargo build --release
	@echo "✓ $(WORKSPACE_ROOT)/target/release/qntx-pty-glyph"

install: build ## Install pty-glyph plugin to ~/.qntx/plugins/
	@echo "Installing qntx-pty-glyph to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp $(WORKSPACE_ROOT)/target/release/qntx-pty-glyph $(PREFIX)/plugins/qntx-pty-glyph
	@chmod +x $(PREFIX)/plugins/qntx-pty-glyph
	@echo "✓ qntx-pty-glyph → $(PREFIX)/plugins/qntx-pty-glyph"

clean: ## Clean build artifacts
	@cargo clean
