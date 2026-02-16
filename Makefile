.PHONY: cli cli-nocgo typegen web run-web test-web test-jsdom test test-coverage test-verbose clean server dev dev-mobile types types-check desktop-prepare desktop-dev desktop-build install proto code-plugin rust-vidstream rust-sqlite rust-embeddings wasm rust-python rust-reduce

# Installation prefix (override with PREFIX=/custom/path make install)
PREFIX ?= $(HOME)/.qntx

# Use prebuilt qntx if available in PATH, otherwise use ./bin/qntx
QNTX := $(shell command -v qntx 2>/dev/null || echo ./bin/qntx)

cli: rust-vidstream rust-sqlite rust-embeddings wasm ## Build QNTX CLI binary (with Rust optimizations, embeddings, and WASM parser)
	@echo "Building QNTX CLI with Rust optimizations (video, sqlite, embeddings) and WASM (parser, fuzzy)..."
	@go build -tags "rustvideo,rustsqlite,rustembeddings,qntxwasm" -ldflags="-X 'github.com/teranos/QNTX/internal/version.VersionTag=$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)' -X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx

cli-nocgo: ## Build QNTX CLI binary without CGO (for Windows or environments without Rust toolchain)
	@echo "Building QNTX CLI (pure Go, no CGO)..."
	@CGO_ENABLED=0 go build -ldflags="-X 'github.com/teranos/QNTX/internal/version.VersionTag=$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)' -X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx

typegen: ## Build standalone typegen binary (pure Go, no plugins/CGO)
	@go build -o bin/typegen ./cmd/typegen

types: proto ## Generate TypeScript, Python, Rust types, CSS symbols, and markdown docs from Go source (via Nix)
	@nix run .#generate-types

types-check: ## Check if generated types are up to date (via Nix)
	@nix run .#check-types

server: cli ## Start QNTX WebSocket server
	@echo "Starting QNTX server..."
	@./bin/qntx server

dev: web cli ## Build frontend and CLI, then start development servers (backend + frontend with live reload)
	@# Read ports from am.toml if exists, otherwise use defaults
	@TOML_BACKEND_PORT=$$(grep -E '^port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	TOML_FRONTEND_PORT=$$(grep -E '^frontend_port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	BACKEND_PORT=$${BACKEND_PORT:-$${TOML_BACKEND_PORT:-8773}}; \
	FRONTEND_PORT=$${FRONTEND_PORT:-$${TOML_FRONTEND_PORT:-8820}}; \
	echo "🚀 Starting development environment..."; \
	echo "  Backend:  http://localhost:$$BACKEND_PORT"; \
	echo "  Frontend: http://localhost:$$FRONTEND_PORT (with live reload)"; \
	echo "  Database: Uses am.toml configuration"; \
	echo "  Override: BACKEND_PORT=<port> FRONTEND_PORT=<port> make dev"; \
	echo ""; \
	lsof -ti:$$BACKEND_PORT | xargs kill -9 2>/dev/null || true; \
	lsof -ti:$$FRONTEND_PORT | xargs kill -9 2>/dev/null || true; \
	trap "echo ''; echo 'Shutting down dev servers...'; \
		test -n \"\$$BACKEND_PID\" && kill -TERM -\$$BACKEND_PID 2>/dev/null || true; \
		test -n \"\$$FRONTEND_PID\" && kill -TERM -\$$FRONTEND_PID 2>/dev/null || true; \
		sleep 1; \
		test -n \"\$$BACKEND_PID\" && kill -9 -\$$BACKEND_PID 2>/dev/null || true; \
		test -n \"\$$FRONTEND_PID\" && kill -9 -\$$FRONTEND_PID 2>/dev/null || true; \
		echo '✓ Servers stopped'" EXIT INT TERM; \
	set -m; \
	./bin/qntx server --dev --no-browser -vvv & \
	BACKEND_PID=$$!; \
	cd web && bun run dev & \
	FRONTEND_PID=$$!; \
	echo "✨ Development servers running"; \
	echo "Press Ctrl+C to stop both servers"; \
	wait

dev-mobile: web cli ## Start dev servers and run iOS app in simulator
	@echo "📱 Starting mobile development environment..."
	@echo "  Backend:  http://localhost:$${BACKEND_PORT:-877}"
	@echo "  Frontend: http://localhost:$${FRONTEND_PORT:-8820} (with live reload)"
	@echo "  iOS:      Launching simulator..."
	@echo ""
	@# Clean up any lingering processes
	@lsof -ti:$${FRONTEND_PORT:-8820} | xargs kill -9 2>/dev/null || true
	@# Start servers in background
	@trap 'echo "Shutting down dev servers..."; \
		test -n "$$BACKEND_PID" && kill -TERM -$$BACKEND_PID 2>/dev/null || true; \
		test -n "$$FRONTEND_PID" && kill -TERM -$$FRONTEND_PID 2>/dev/null || true; \
		lsof -ti:$${FRONTEND_PORT:-8820} | xargs kill -9 2>/dev/null || true; \
		wait 2>/dev/null || true; \
		echo "✓ Servers stopped cleanly"' INT; \
	./bin/qntx server --dev --no-browser -vvv & \
	BACKEND_PID=$$!; \
	cd web && bun run dev & \
	FRONTEND_PID=$$!; \
	sleep 3; \
	echo "✨ Servers running, launching iOS app..."; \
	cd web/src-tauri && SKIP_DEV_SERVER=1 cargo tauri ios dev "iPhone 17 Pro"; \
	wait

web: wasm ## Build web assets with Bun (requires WASM)
	@echo "Building web assets..."
	@cd web && bun install && bun run build

run-web: ## Run web dev server
	@echo "Starting web dev server..."
	@cd web && bun run dev

test-web: ## Run web UI tests
	@echo "Running web UI tests..."
	@cd web && bun test

test-jsdom: ## Run web UI tests including JSDOM DOM tests
	@echo "Running web UI tests with JSDOM..."
	@if [ ! -d "web/node_modules" ]; then \
		echo "Installing web dependencies..."; \
		cd web && bun install; \
	fi
	@cd web && USE_JSDOM=1 bun test

test: ## Run all tests (Go + TypeScript)
	@go test -tags "rustsqlite,qntxwasm" -short ./...
	@if [ ! -d "web/node_modules" ]; then \
		cd web && bun install; \
	fi
	@cd web && USE_JSDOM=1 bun test
	@echo "✓ All tests complete"

test-coverage: ## Run all tests (Go + TypeScript) with coverage
	@echo "Running Go tests with coverage..."
	@mkdir -p tmp
	@# Test with core tags to ensure we test what we ship
	@go test -tags "rustsqlite,qntxwasm" -short -coverprofile=tmp/coverage.out -covermode=count ./...
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
	# TODO: Add proper Nix package for Tauri desktop app (rustPlatform.buildRustPackage)
	# This would eliminate build complexity and ensure reproducible builds across environments
	@echo "Preparing desktop app assets..."
	@./web/src-tauri/generate-icons.sh
	@./web/src-tauri/prepare-sidecar.sh
	@echo "✓ Desktop app prepared"

desktop-dev: desktop-prepare ## Run desktop app in development mode
	@echo "Starting QNTX Desktop in development mode..."
	@echo "  Frontend dev server: http://localhost:$${FRONTEND_PORT:-8820}"
	@echo "  Backend will start as sidecar on port $${BACKEND_PORT:-877}"
	@echo ""
	@# Clean up any lingering dev server processes
	@lsof -ti:$${FRONTEND_PORT:-8820} | xargs kill -9 2>/dev/null || true
	@# Start dev server in background, then launch Tauri
	@trap 'echo "Shutting down dev server..."; \
		lsof -ti:$${FRONTEND_PORT:-8820} | xargs kill -9 2>/dev/null || true; \
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

proto: ## Generate Go code from protobuf definitions (via Nix)
	@nix run .#generate-proto

proto-rust: ## Rust proto types are now generated automatically at build time
	@echo "ℹ️  Rust proto types are generated automatically when building qntx-proto"
	@echo "   No manual generation needed - uses protoc-bin-vendored at build time"
	@echo "   See: crates/qntx-proto/build.rs"

code-plugin: ## Build and install code plugin to ~/.qntx/plugins/
	@echo "Building code plugin..."
	@mkdir -p $(PREFIX)/plugins
	@go build -o $(PREFIX)/plugins/qntx-code-plugin ./qntx-code/cmd/qntx-code-plugin
	@chmod +x $(PREFIX)/plugins/qntx-code-plugin
	@echo "✓ qntx-code-plugin → $(PREFIX)/plugins/qntx-code-plugin"

rust-vidstream: ## Build Rust vidstream library with ONNX support (for CGO integration)
	@echo "Building Rust vidstream library with ONNX..."
	@cd ats/vidstream && cargo build --release --features onnx --lib
	@echo "✓ libqntx_vidstream built in ats/vidstream/target/release/"
	@echo "  Static:  libqntx_vidstream.a"
	@echo "  Shared:  libqntx_vidstream.so (Linux) / libqntx_vidstream.dylib (macOS)"
	@echo "  Features: ONNX Runtime (download-binaries enabled)"

rust-sqlite: ## Build Rust SQLite storage library with FFI support (for CGO integration)
	@echo "Building Rust SQLite storage library..."
	@cargo build --release --package qntx-sqlite --features ffi --lib
	@echo "✓ libqntx_sqlite built in target/release/"
	@echo "  Static:  libqntx_sqlite.a"
	@echo "  Shared:  libqntx_sqlite.so (Linux) / libqntx_sqlite.dylib (macOS)"

wasm: ## Build qntx-core as WASM module (for wazero integration + browser)
	@echo "Building qntx-core WASM modules..."
	@echo "  [1/2] Building Go/wazero WASM..."
	@cargo build --release --target wasm32-unknown-unknown --package qntx-wasm
	@cp target/wasm32-unknown-unknown/release/qntx_wasm.wasm ats/wasm/qntx_core.wasm
	@echo "  ✓ qntx_core.wasm built and copied to ats/wasm/"
	@ls -lh ats/wasm/qntx_core.wasm | awk '{print "    Size: " $$5}'
	@echo "  [2/2] Building browser WASM with wasm-bindgen..."
	@if ! command -v wasm-pack >/dev/null 2>&1; then \
		echo "  ⚠️  wasm-pack not found. Install with: cargo install wasm-pack"; \
		exit 1; \
	fi
	@cd crates/qntx-wasm && wasm-pack build --target web --features browser
	@cp -r crates/qntx-wasm/pkg/* web/wasm/
	@echo "  ✓ Browser WASM built and copied to web/wasm/"
	@ls -lh web/wasm/*.wasm 2>/dev/null | awk '{print "    Size: " $$5 " - " $$9}' || (echo "    ERROR: wasm-pack ran but produced no .wasm files"; exit 1)

rust-embeddings: ## Build Rust embeddings library with ONNX support (for CGO integration)
	@echo "Building Rust embeddings library with ONNX..."
	@cd ats/embeddings && cargo build --release --features ffi --lib
	@echo "✓ libqntx_embeddings built in ats/embeddings/target/release/"
	@echo "  Static:  libqntx_embeddings.a"
	@echo "  Shared:  libqntx_embeddings.so (Linux) / libqntx_embeddings.dylib (macOS)"
	@echo "  Features: ONNX Runtime for sentence transformers"

# Rust Python plugin (PyO3-based Python execution)
# REQUIRES Nix: Platform-specific Python linking issues make cargo-only builds unreliable
rust-python: ## Build Rust Python plugin binary (via Nix)
	@echo "Building qntx-python-plugin via Nix..."
	@nix build ./qntx-python#qntx-python-plugin
	@mkdir -p bin
	@cp -L result/bin/qntx-python-plugin bin/
	@echo "✓ qntx-python-plugin built in bin/"

# Rust Reduce plugin (PyO3-based UMAP dimensionality reduction)
# REQUIRES Nix: Python linking + umap-learn dependency
rust-reduce: ## Build Rust Reduce plugin binary (via Nix)
	@echo "Building qntx-reduce-plugin via Nix..."
	@nix build ./qntx-reduce#qntx-reduce-plugin
	@mkdir -p bin
	@rm -f bin/qntx-reduce-plugin
	@cp -L result/bin/qntx-reduce-plugin bin/
	@chmod +x bin/qntx-reduce-plugin
	@echo "✓ qntx-reduce-plugin built in bin/"

rust-reduce-install: rust-reduce ## Install Rust Reduce plugin to ~/.qntx/plugins/
	@echo "Installing qntx-reduce-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@rm -f $(PREFIX)/plugins/qntx-reduce-plugin
	@cp bin/qntx-reduce-plugin $(PREFIX)/plugins/
	@chmod +x $(PREFIX)/plugins/qntx-reduce-plugin
	@echo "✓ qntx-reduce-plugin installed to $(PREFIX)/plugins/"

rust-python-install: rust-python ## Install Rust Python plugin to ~/.qntx/plugins/
	@echo "Installing qntx-python-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@rm -f $(PREFIX)/plugins/qntx-python-plugin
	@cp bin/qntx-python-plugin $(PREFIX)/plugins/
	@chmod +x $(PREFIX)/plugins/qntx-python-plugin
	@echo "✓ qntx-python-plugin installed to $(PREFIX)/plugins/"
