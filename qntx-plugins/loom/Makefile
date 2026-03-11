.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build loom plugin binary
	@echo "Building qntx-loom-plugin..."
	@opam exec -- dune build
	@echo "✓ _build/default/bin/main.exe"

install: build ## Install loom plugin to ~/.qntx/plugins/
	@echo "Installing qntx-loom-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@rm -f $(PREFIX)/plugins/qntx-loom-plugin
	@cp _build/default/bin/main.exe $(PREFIX)/plugins/qntx-loom-plugin
	@chmod +x $(PREFIX)/plugins/qntx-loom-plugin
	@echo "✓ qntx-loom-plugin → $(PREFIX)/plugins/qntx-loom-plugin"

clean: ## Clean build artifacts
	@dune clean
