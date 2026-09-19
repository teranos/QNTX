.PHONY: build build-module install clean

PREFIX ?= $(HOME)/.qntx
WORKSPACE_ROOT := $(shell cd ../.. && pwd)

build-module: ## Compile TypeScript element module to JS for include_str!
	@cd web && bun install --frozen-lockfile
	@bun build web/pty-element-module.ts --outfile web/pty-element-module.js --target browser
	@cp web/node_modules/@xterm/xterm/css/xterm.css web/xterm.css

build: build-module ## Build pty-element plugin binary
	@echo "Building qntx-pty-element..."
	@cargo build --release
	@echo "✓ $(WORKSPACE_ROOT)/target/release/qntx-pty-element"

install: build ## Install pty-element plugin to ~/.qntx/plugins/
	@echo "Installing qntx-pty-element to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp $(WORKSPACE_ROOT)/target/release/qntx-pty-element $(PREFIX)/plugins/qntx-pty-element
	@chmod +x $(PREFIX)/plugins/qntx-pty-element
	@echo "✓ qntx-pty-element → $(PREFIX)/plugins/qntx-pty-element"

clean: ## Clean build artifacts
	@cargo clean
