.PHONY: build install clean shared-lib

PREFIX ?= $(HOME)/.qntx
SHARED_LIB_DIR := ../../plugin/grpc/ocaml

shared-lib: ## Install shared qntx-plugin library into opam switch
	@echo "Installing shared qntx-plugin library..."
	@cd $(SHARED_LIB_DIR) && opam exec -- dune build && opam exec -- dune install 2>/dev/null
	@echo "✓ qntx-plugin installed"

build: shared-lib ## Build kern plugin binary
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
	@opam exec -- dune clean
