.PHONY: cli cli-nocgo web run-web test-web test test-verbose clean server dev dev-mobile types types-check desktop-prepare desktop-dev desktop-build install proto plugins rust-fuzzy rust-fuzzy-test rust-fuzzy-check

# Installation prefix (override with PREFIX=/custom/path make install)
PREFIX ?= $(HOME)/.qntx

# Use prebuilt qntx if available in PATH, otherwise use ./bin/qntx
QNTX := $(shell command -v qntx 2>/dev/null || echo ./bin/qntx)

cli: rust-fuzzy ## Build QNTX CLI binary (with Rust fuzzy optimization)
	@echo "Building QNTX CLI with Rust fuzzy optimization..."
	@go build -tags rustfuzzy -ldflags="-X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx

cli-nocgo: ## Build QNTX CLI binary without CGO (for Windows or environments without Rust toolchain)
	@echo "Building QNTX CLI (pure Go, no CGO)..."
	@CGO_ENABLED=0 go build -ldflags="-X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx

types: $(if $(findstring ./bin/qntx,$(QNTX)),cli-nocgo,) ## Generate TypeScript, Python, Rust types and markdown docs from Go source
	@echo "Generating types and documentation..."
	@$(QNTX) typegen --lang typescript --output types/generated/
	@$(QNTX) typegen --lang python --output types/generated/
	@$(QNTX) typegen --lang rust --output types/generated/
	@$(QNTX) typegen --lang markdown  # Defaults to docs/types/
	@echo "✓ TypeScript types generated in types/generated/typescript/"
	@echo "✓ Python types generated in types/generated/python/"
	@echo "✓ Rust types generated in types/generated/rust/"
	@echo "✓ Markdown docs generated in docs/types/"

types-check: cli ## Check if generated types are up to date (always builds from source)
	@./bin/qntx typegen check

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
	@echo "Running TypeScript tests with coverage..."
	@if [ ! -d "web/node_modules" ]; then \
		echo "Installing web dependencies..."; \
		cd web && bun install; \
	fi
	@cd web && bun test --coverage
	@echo "✓ All tests complete"

test-verbose: ## Run all tests (Go + TypeScript) with verbose output and coverage
	@echo "Running Go tests with verbose output..."
	@mkdir -p tmp
	@go test -v -coverprofile=tmp/coverage.out -covermode=count ./...
	@go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "✓ Go tests complete. Coverage report: tmp/coverage.html"
	@echo ""
	@echo "Running TypeScript tests with coverage..."
	@if [ ! -d "web/node_modules" ]; then \
		echo "Installing web dependencies..."; \
		cd web && bun install; \
	fi
	@cd web && bun test --coverage
	@echo "✓ All tests complete"

clean: ## Clean build artifacts
	@rm -rf internal/server/dist
	@rm -rf web/node_modules
	@rm -rf plugins/qntx-fuzzy/target

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
	@echo ""
	@# Clean up any lingering dev server processes
	@pkill -f "bun.*dev" 2>/dev/null || true
	@lsof -ti:8820 | xargs kill -9 2>/dev/null || true
	@# Start dev server in background, then launch Tauri
	@trap 'echo "Shutting down dev server..."; \
		pkill -f "bun.*dev" 2>/dev/null || true; \
		wait 2>/dev/null || true; \
		echo "✓ Dev server stopped"' INT; \
	cd web && bun run dev & \
	DEV_SERVER_PID=$$!; \
	sleep 2; \
	echo "✨ Dev server running, launching desktop app..."; \
	cd web/src-tauri && SKIP_DEV_SERVER=1 cargo run; \
	kill $$DEV_SERVER_PID 2>/dev/null || true; \
	wait

desktop-build: desktop-prepare ## Build production desktop app (requires: cargo install tauri-cli)
	@echo "Building QNTX Desktop for production..."
	@cd web/src-tauri && cargo tauri build
	@# Workaround: Manually copy sidecar to bundle (Tauri v2 bundling issue)
	@echo "Bundling sidecar binary..."
	@TARGET=$$(rustc -vV | grep host | cut -d' ' -f2) && \
		cp web/src-tauri/bin/qntx-$$TARGET target/release/bundle/macos/QNTX.app/Contents/MacOS/
	@echo "✓ Desktop app built in target/release/bundle/"

proto: ## Generate Go code from protobuf definitions
	@echo "Generating gRPC code from proto files..."
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		plugin/grpc/protocol/domain.proto
	@echo "✓ Proto files generated in plugin/grpc/protocol/"

plugins: ## Build and install all external plugin binaries to ~/.qntx/plugins/
	@echo "Building external plugins..."
	@mkdir -p $(PREFIX)/plugins
	@# Build code plugin from qntx-code/cmd/qntx-code-plugin
	@if [ -d "qntx-code/cmd/qntx-code-plugin" ]; then \
		echo "  Building code (Go) plugin..."; \
		go build -o $(PREFIX)/plugins/qntx-code-plugin ./qntx-code/cmd/qntx-code-plugin || exit 1; \
		chmod +x $(PREFIX)/plugins/qntx-code-plugin; \
		echo "  ✓ qntx-code-plugin → $(PREFIX)/plugins/qntx-code-plugin"; \
	fi
	@# Install Python/script-based plugins (e.g., webscraper)
	@if [ -d "qntx-webscraper" ]; then \
		echo "  Installing webscraper (Python) plugin..."; \
		cp qntx-webscraper/webscraper $(PREFIX)/plugins/webscraper; \
		chmod +x $(PREFIX)/plugins/webscraper; \
		cp qntx-webscraper/webscraper.toml $(PREFIX)/plugins/webscraper.toml 2>/dev/null || true; \
		echo "  ✓ webscraper → $(PREFIX)/plugins/webscraper"; \
	fi
	@echo "✓ All plugins installed to $(PREFIX)/plugins/"
	@echo ""
	@echo "Installed plugins:"
	@ls -lh $(PREFIX)/plugins/qntx-*-plugin $(PREFIX)/plugins/webscraper 2>/dev/null || echo "  (none)"

# Rust fuzzy matching library (ax segment optimization)
rust-fuzzy: ## Build Rust fuzzy matching library (for CGO integration)
	@echo "Building Rust fuzzy matching library..."
	@cd ats/ax/fuzzy-ax && cargo build --release --lib
	@echo "✓ libqntx_fuzzy built in ats/ax/fuzzy-ax/target/release/"
	@echo "  Static:  libqntx_fuzzy.a"
	@echo "  Shared:  libqntx_fuzzy.so (Linux) / libqntx_fuzzy.dylib (macOS)"

rust-fuzzy-test: ## Run Rust fuzzy matching tests
	@echo "Running Rust fuzzy matching tests..."
	@cd ats/ax/fuzzy-ax && cargo test --lib
	@echo "✓ All Rust tests passed"

rust-fuzzy-check: ## Check Rust fuzzy matching code (fmt + clippy)
	@echo "Checking Rust fuzzy matching code..."
	@cd ats/ax/fuzzy-ax && cargo fmt --check
	@cd ats/ax/fuzzy-ax && cargo clippy --lib -- -D warnings
	@echo "✓ Rust code checks passed"

rust-fuzzy-integration: rust-fuzzy ## Run Rust fuzzy integration tests (Go + Rust)
	@echo "Running Rust fuzzy integration tests..."
	@export DYLD_LIBRARY_PATH=$(PWD)/ats/ax/fuzzy-ax/target/release:$$DYLD_LIBRARY_PATH && \
		export LD_LIBRARY_PATH=$(PWD)/ats/ax/fuzzy-ax/target/release:$$LD_LIBRARY_PATH && \
		go test -tags "integration rustfuzzy" -v ./ats/ax/fuzzy-ax/...
	@echo "✓ Integration tests passed"
