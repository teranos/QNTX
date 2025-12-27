.PHONY: cli web run-web test-web test clean

cli: ## Build QNTX CLI binary
	@echo "Building QNTX CLI..."
	@go build -o bin/qntx ./cmd/qntx

web: ## Build web assets with Bun
	@echo "Building web assets..."
	@cd web && bun install && bun run build

run-web: ## Run web dev server
	@echo "Starting web dev server..."
	@cd web && bun run dev

test-web: ## Run web UI tests
	@echo "Running web UI tests..."
	@cd web && bun test

test: ## Run all tests
	@go test ./...

clean: ## Clean build artifacts
	@rm -rf internal/server/dist
	@rm -rf web/node_modules
