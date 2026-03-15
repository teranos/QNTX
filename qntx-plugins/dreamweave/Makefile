.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx

build: ## Build dreamweave plugin binary
	@echo "Building qntx-dreamweave-plugin..."
	@opam exec -- dune build
	@echo "✓ _build/default/bin/main.exe"

install: build ## Install dreamweave plugin to ~/.qntx/plugins/
	@echo "Installing qntx-dreamweave-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@rm -f $(PREFIX)/plugins/qntx-dreamweave-plugin
	@cp _build/default/bin/main.exe $(PREFIX)/plugins/qntx-dreamweave-plugin
	@chmod +x $(PREFIX)/plugins/qntx-dreamweave-plugin
	@echo "✓ qntx-dreamweave-plugin → $(PREFIX)/plugins/qntx-dreamweave-plugin"

clean: ## Clean build artifacts
	@dune clean
