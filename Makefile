.PHONY: cli web run-web test-web test test-verbose clean server dev types types-check desktop-prepare desktop-dev desktop-build

cli: ## Build QNTX CLI binary
	@echo "Building QNTX CLI..."
	@go build -ldflags="-X 'github.com/teranos/QNTX/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx

types: cli ## Generate TypeScript, Rust types and markdown docs from Go source
	@echo "Generating types and documentation..."
	@./bin/qntx typegen --lang typescript --output types/generated/
	@./bin/qntx typegen --lang rust --output types/generated/
	@./bin/qntx typegen --lang markdown  # Defaults to docs/types/
	@echo "✓ TypeScript types generated in types/generated/typescript/"
	@echo "✓ Rust types generated in types/generated/rust/"
	@echo "✓ Markdown docs generated in docs/types/"

types-check: cli ## Check if generated types are up to date
	@echo "Checking generated types..."
	@mkdir -p tmp/types-check
	@./bin/qntx typegen --lang typescript --output tmp/types-check/
	@./bin/qntx typegen --lang rust --output tmp/types-check/
	@./bin/qntx typegen --lang markdown --output tmp/types-check/
	@if diff -r tmp/types-check/typescript types/generated/typescript > /dev/null 2>&1 && \
	   diff -r tmp/types-check/rust types/generated/rust > /dev/null 2>&1 && \
	   diff -r tmp/types-check/markdown docs/types > /dev/null 2>&1; then \
		echo "✓ Types are up to date"; \
		rm -rf tmp/types-check; \
	else \
		echo "✗ Types are out of date. Run 'make types' to regenerate."; \
		rm -rf tmp/types-check; \
		exit 1; \
	fi

server: cli ## Start QNTX WebSocket server
	@echo "Starting QNTX server..."
	@./bin/qntx server

dev: web cli ## Build frontend and CLI, then start development servers (backend + frontend with live reload)
	@echo "🚀 Starting development environment..."
	@echo "  Backend:  http://localhost:877"
	@echo "  Frontend: http://localhost:8820 (with live reload)"
	@echo "  Database: dev-qntx.db (development)"
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
	DB_PATH=dev-qntx.db ./bin/qntx server --dev --no-browser -vvv & \
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

test: ## Run all tests (Go + TypeScript) with coverage
	@echo "Running Go tests with coverage..."
	@mkdir -p tmp
	@go test -short -coverprofile=tmp/coverage.out -covermode=count ./...
	@go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "✓ Go tests complete. Coverage report: tmp/coverage.html"
	@echo ""
	@echo "Running TypeScript tests..."
	@cd web && bun test
	@echo "✓ All tests complete"

test-verbose: ## Run all tests (Go + TypeScript) with verbose output and coverage
	@echo "Running Go tests with verbose output..."
	@mkdir -p tmp
	@go test -v -coverprofile=tmp/coverage.out -covermode=count ./...
	@go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "✓ Go tests complete. Coverage report: tmp/coverage.html"
	@echo ""
	@echo "Running TypeScript tests..."
	@cd web && bun test
	@echo "✓ All tests complete"

clean: ## Clean build artifacts
	@rm -rf internal/server/dist
	@rm -rf web/node_modules

desktop-prepare: cli web ## Prepare desktop app (icons + sidecar binary)
	@echo "Preparing desktop app assets..."
	@./web/src-tauri/generate-icons.sh
	@./web/src-tauri/prepare-sidecar.sh
	@echo "✓ Desktop app prepared"

desktop-dev: desktop-prepare ## Run desktop app in development mode
	@echo "Starting QNTX Desktop in development mode..."
	@cd web && bun run tauri:dev

desktop-build: desktop-prepare ## Build production desktop app
	@echo "Building QNTX Desktop for production..."
	@cd web && bun run tauri:build
	@echo "✓ Desktop app built in web/src-tauri/target/release/bundle/"
