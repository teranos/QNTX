.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build kern plugin binary
	@echo "Building qntx-kern-plugin..."
	@opam exec -- dune build
	@echo "✓ _build/default/bin/main.exe"

install: build ## Install kern plugin to ~/.qntx/plugins/
	@echo "Installing qntx-kern-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@rm -f $(PREFIX)/plugins/qntx-kern-plugin
	@cp _build/default/bin/main.exe $(PREFIX)/plugins/qntx-kern-plugin
	@chmod +x $(PREFIX)/plugins/qntx-kern-plugin
	@echo "✓ qntx-kern-plugin → $(PREFIX)/plugins/qntx-kern-plugin"

clean: ## Clean build artifacts
	@dune clean
