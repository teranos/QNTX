.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx
WORKSPACE_ROOT := $(shell cd ../.. && pwd)

build: ## Build pty-glyph plugin binary
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
