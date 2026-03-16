.PHONY: build install clean shared-lib

PREFIX ?= $(HOME)/.qntx
SHARED_LIB_DIR := ../../plugin/grpc/ocaml

shared-lib: ## Install shared qntx-plugin library into opam switch
	@echo "Installing shared qntx-plugin library..."
	@cd $(SHARED_LIB_DIR) && opam exec -- dune build && opam exec -- dune install 2>/dev/null
	@echo "✓ qntx-plugin installed"

build: shared-lib ## Build loom plugin binary
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
	@opam exec -- dune clean
