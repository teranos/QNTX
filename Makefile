.PHONY: cli web run-web test-web test test-verbose clean server dev dev-mobile types types-check desktop-prepare desktop-dev desktop-build install

# Installation prefix (override with PREFIX=/custom/path make install)
PREFIX ?= $(HOME)/.qntx

# Use prebuilt qntx if available in PATH, otherwise use ./bin/qntx
QNTX := $(shell command -v qntx 2>/dev/null || echo ./bin/qntx)

cli: ## Build QNTX CLI binary
	@echo "Building QNTX CLI..."
	@go build -ldflags="-X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx

types: $(if $(findstring ./bin/qntx,$(QNTX)),cli,) ## Generate TypeScript, Python, Rust types and markdown docs from Go source
	@echo "Generating types and documentation..."
	@$(QNTX) typegen --lang typescript --output types/generated/
	@$(QNTX) typegen --lang python --output types/generated/
	@$(QNTX) typegen --lang rust --output types/generated/
	@$(QNTX) typegen --lang markdown  # Defaults to docs/types/
	@echo "✓ TypeScript types generated in types/generated/typescript/"
	@echo "✓ Python types generated in types/generated/python/"
	@echo "✓ Rust types generated in types/generated/rust/"
	@echo "✓ Markdown docs generated in docs/types/"

types-check: $(if $(findstring ./bin/qntx,$(QNTX)),cli,) ## Check if generated types are up to date
	@$(QNTX) typegen check

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

dev-mobile: web cli ## Start dev servers and run iOS app in simulator
	@echo "📱 Starting mobile development environment..."
	@echo "  Backend:  http://localhost:877"
	@echo "  Frontend: http://localhost:8820 (with live reload)"
	@echo "  iOS:      Launching simulator..."
	@echo ""
	@# Clean up any lingering processes
	@pkill -f "bun.*dev" 2>/dev/null || true
	@lsof -ti:8820 | xargs kill -9 2>/dev/null || true
	@# Start servers in background
	@trap 'echo "Shutting down dev servers..."; \
		test -n "$$BACKEND_PID" && kill -TERM -$$BACKEND_PID 2>/dev/null || true; \
		test -n "$$FRONTEND_PID" && kill -TERM -$$FRONTEND_PID 2>/dev/null || true; \
		pkill -f "bun.*dev" 2>/dev/null || true; \
		wait 2>/dev/null || true; \
		echo "✓ Servers stopped cleanly"' INT; \
	DB_PATH=dev-qntx.db ./bin/qntx server --dev --no-browser -vvv & \
	BACKEND_PID=$$!; \
	cd web && bun run dev & \
	FRONTEND_PID=$$!; \
	sleep 3; \
	echo "✨ Servers running, launching iOS app..."; \
	cd web/src-tauri && SKIP_DEV_SERVER=1 cargo tauri ios dev "iPhone 17 Pro"; \
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

install: cli ## Install QNTX binary to ~/.qntx/bin (override with PREFIX=/custom/path)
	@echo "Installing qntx to $(PREFIX)/bin..."
	@mkdir -p $(PREFIX)/bin
	@cp bin/qntx $(PREFIX)/bin/qntx
	@chmod +x $(PREFIX)/bin/qntx
	@echo "✓ qntx installed to $(PREFIX)/bin/qntx"
	@if ! echo $$PATH | grep -q "$(PREFIX)/bin"; then \
		echo ""; \
		echo "⚠️  $(PREFIX)/bin is not in your PATH"; \
		echo "Add this to your shell config:"; \
		echo "  export PATH=\"$(PREFIX)/bin:\$$PATH\""; \
	fi

desktop-prepare: cli web ## Prepare desktop app (icons + sidecar binary)
	@echo "Preparing desktop app assets..."
	@./web/src-tauri/generate-icons.sh
	@./web/src-tauri/prepare-sidecar.sh
	@echo "✓ Desktop app prepared"

desktop-dev: desktop-prepare ## Run desktop app in development mode
	@echo "Starting QNTX Desktop in development mode..."
	@echo "  Frontend dev server: http://localhost:8820"
	@echo "  Backend will start as sidecar on port 877"
	@cd web/src-tauri && cargo run

desktop-build: desktop-prepare ## Build production desktop app (requires: cargo install tauri-cli)
	@echo "Building QNTX Desktop for production..."
	@cd web/src-tauri && cargo tauri build
	@echo "✓ Desktop app built in target/release/bundle/"
