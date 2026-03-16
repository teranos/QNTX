.PHONY: build install clean shared-lib

PREFIX ?= $(HOME)/.qntx
SHARED_LIB_DIR := ../../plugin/grpc/ocaml

shared-lib: ## Install shared qntx-plugin library into opam switch
	@echo "Installing shared qntx-plugin library..."
	@cd $(SHARED_LIB_DIR) && opam exec -- dune build && opam exec -- dune install 2>/dev/null
	@echo "✓ qntx-plugin installed"

build: shared-lib ## Build dreamweave plugin binary
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
	@opam exec -- dune clean
