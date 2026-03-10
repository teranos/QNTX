.PHONY: build-module clean

build-module: ## Compile TypeScript glyph module to JS for plugin serving
	@cd web && bun install --frozen-lockfile
	@mkdir -p web/dist
	@bun build web/graph-view-module.ts --outfile web/dist/graph-view-module.js --target browser

clean: ## Clean build artifacts
	@rm -rf web/dist web/node_modules
