.PHONY: cli web run-web test-web test test-verbose clean server dev

cli: ## Build QNTX CLI binary
	@echo "Building QNTX CLI..."
	@go build -o bin/qntx ./cmd/qntx

server: cli ## Start QNTX WebSocket server
	@echo "Starting QNTX server..."
	@./bin/qntx server

dev: web cli ## Build frontend and CLI, then start development servers (backend + frontend with live reload)
	@echo "🚀 Starting development environment..."
	@echo "  Backend:  http://localhost:877"
	@echo "  Frontend: http://localhost:8820 (with live reload)"
	@echo ""
	@# Clean up any lingering processes on dev ports
	@pkill -f "bun.*dev" 2>/dev/null || true
	@lsof -ti:8820 | xargs kill -9 2>/dev/null || true
	@trap 'echo "Shutting down dev servers..."; \
		test -n "$$BACKEND_PID" && kill -TERM -$$BACKEND_PID 2>/dev/null || true; \
		test -n "$$FRONTEND_PID" && kill -TERM -$$FRONTEND_PID 2>/dev/null || true; \
		pkill -f "bun.*dev" 2>/dev/null || true; \
		wait 2>/dev/null || true; \
		echo "✓ Servers stopped cleanly"' INT; \
	set -m; \
	./bin/qntx server --dev --no-browser -vvv & \
	BACKEND_PID=$$!; \
	cd web && bun run dev & \
	FRONTEND_PID=$$!; \
	echo "✨ Development servers running"; \
	echo "Press Ctrl+C to stop both servers"; \
	wait

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
