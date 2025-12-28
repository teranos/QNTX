.PHONY: cli web run-web test-web test test-verbose clean server

cli: ## Build QNTX CLI binary
	@echo "Building QNTX CLI..."
	@go build -o bin/qntx ./cmd/qntx

server: cli ## Start QNTX WebSocket server
	@echo "Starting QNTX server..."
	@./bin/qntx server

web: ## Build web assets with Bun
	@echo "Building web assets..."
	@cd web && bun install && bun run build

run-web: ## Run web dev server
	@echo "Starting web dev server..."
	@cd web && bun run dev

test-web: ## Run web UI tests
	@echo "Running web UI tests..."
	@cd web && bun test

test: ## Run all tests with coverage
	@echo "Running tests with coverage..."
	@mkdir -p tmp
	@go test -short -coverprofile=tmp/coverage.out -covermode=count ./...
	@go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "✓ Tests complete. Coverage report: tmp/coverage.html"

test-verbose: ## Run all tests with verbose output and coverage
	@echo "Running tests with verbose output..."
	@mkdir -p tmp
	@go test -v -coverprofile=tmp/coverage.out -covermode=count ./...
	@go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "✓ Verbose tests complete. Coverage report: tmp/coverage.html"

clean: ## Clean build artifacts
	@rm -rf internal/server/dist
	@rm -rf web/node_modules
