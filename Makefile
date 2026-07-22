.PHONY: cli typegen web run-web test-web test-jsdom test test-ocaml test-d test-coverage test-verbose clean server dev dev-mobile types types-check desktop-prepare desktop-dev desktop-build install proto code-plugin atproto-plugin github-plugin ix-json-plugin ix-bin-plugin ix-net-plugin faal-plugin openrouter-plugin pty-glyph-plugin loom-plugin kern-plugin llama-cpp-plugin meili-plugin rust-sqlite wasm rust-reduce

# Installation prefix (override with PREFIX=/custom/path make install)
PREFIX ?= $(HOME)/.qntx

# Use prebuilt qntx if available in PATH, otherwise use ./bin/qntx
QNTX := $(shell command -v qntx 2>/dev/null || echo ./bin/qntx)

# Ground immediate delivery — notify active Claude sessions of build progress.
# Usage: @$(call ground-notify,name,detail message)
GROUND_DB := $(HOME)/.local/share/ground/ground.db
define ground-notify
	@if [ -f "$(GROUND_DB)" ]; then \
		TS=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
		sqlite3 "$(GROUND_DB)" "INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes) VALUES ('make-$(1)-' || strftime('%s','now'), '[\"qntx\"]', '[\"immediate:$(1)\"]', '[\"project:teranos/QNTX\"]', '[\"make\"]', '$$TS', 'make', json_object('detail', '$(2) at ' || time('now','localtime'), 'after', 0))"; \
	fi
endef

# Optional: KERN=1 make cli/dev to enable OCaml parser plugin
BUILD_TAGS := rustsqlite,qntxwasm
ifdef KERN
BUILD_TAGS := $(BUILD_TAGS),kern
endif

cli: rust-sqlite wasm ## Build QNTX CLI binary (with Rust optimizations and WASM parser)
	@echo "Building QNTX CLI with Rust optimizations (sqlite) and WASM (parser, fuzzy)..."
	$(call ground-notify,go-build,Go: building qntx cli)
	@go build -tags "$(BUILD_TAGS)" -ldflags="-X 'github.com/teranos/QNTX/internal/version.VersionTag=$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)' -X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx || { \
		if [ -f "$(GROUND_DB)" ]; then sqlite3 "$(GROUND_DB)" "INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes) VALUES ('make-go-build-failed-$$(date +%s)', '[\"qntx\"]', '[\"immediate:go-build-failed\"]', '[\"project:teranos/QNTX\"]', '[\"make\"]', '$$(date -u +%Y-%m-%dT%H:%M:%SZ)', 'make', '{\"detail\":\"Go: qntx cli build FAILED\",\"after\":0}')"; fi; \
		exit 1; }

typegen: ## Install typegen binary from github.com/teranos/typegen
	@go install github.com/teranos/typegen/cmd/typegen@latest
	@cp $(shell go env GOPATH)/bin/typegen bin/typegen

types: proto ## Generate TypeScript, Python, Rust types, CSS symbols, and markdown docs from Go source (via Nix)
	@nix run .#generate-types

types-check: ## Check if generated types are up to date (via Nix)
	@nix run .#check-types

server: cli ## Start QNTX WebSocket server
	@echo "Starting QNTX server..."
	@./bin/qntx server

dev: ## Build frontend and CLI, then start development servers (backend + frontend with live reload)
	$(call ground-notify,rebuilding,make dev: rebuilding QNTX)
	@$(MAKE) web cli
	@# Read ports from am.toml if exists, otherwise use defaults
	@TOML_BACKEND_PORT=$$(grep -E '^port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	TOML_FRONTEND_PORT=$$(grep -E '^frontend_port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	BACKEND_PORT=$${BACKEND_PORT:-$${TOML_BACKEND_PORT:-87700}}; \
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
	GOTRACEBACK=crash ./bin/qntx server --dev --no-browser -vvv 2> tmp/qntx-crash.log & \
	BACKEND_PID=$$!; \
	cd web && bun run dev & \
	FRONTEND_PID=$$!; \
	echo "✨ Development servers running"; \
	echo "Press Ctrl+C to stop both servers"; \
	wait

dave: web cli ## Start QNTX backend + frontend (daemonized, for Claude Code)
	@TOML_BACKEND_PORT=$$(grep -E '^port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	TOML_FRONTEND_PORT=$$(grep -E '^frontend_port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	BACKEND_PORT=$${BACKEND_PORT:-$${TOML_BACKEND_PORT:-8770}}; \
	FRONTEND_PORT=$${FRONTEND_PORT:-$${TOML_FRONTEND_PORT:-8820}}; \
	lsof -ti:$$BACKEND_PORT | xargs kill -9 2>/dev/null || true; \
	lsof -ti:$$FRONTEND_PORT | xargs kill -9 2>/dev/null || true; \
	sleep 1; \
	GOTRACEBACK=crash nohup ./bin/qntx server --dev --no-browser -vvv > tmp/qntx-$$BACKEND_PORT.log 2> tmp/qntx-crash.log & \
	BPID=$$!; \
	echo $$BPID > tmp/qntx.pid; \
	(cd web && nohup bun run dev > ../tmp/frontend.log 2>&1 &) & \
	FPID=$$!; \
	echo $$FPID > tmp/frontend.pid; \
	for i in 1 2 3 4 5 6 7 8 9 10; do \
		if curl -sf http://127.0.0.1:$$BACKEND_PORT/api/plugins > /dev/null 2>&1; then \
			echo "QNTX running on port $$BACKEND_PORT (pid $$BPID)"; \
			echo "Frontend running on port $$FRONTEND_PORT (pid $$FPID)"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "QNTX started (pid $$BPID) but not yet responding on port $$BACKEND_PORT — check tmp/qntx-$$BACKEND_PORT.log"

stopdave: ## Stop daemonized QNTX + frontend (from dave)
	@for PIDFILE in tmp/qntx.pid tmp/frontend.pid; do \
		if [ -f $$PIDFILE ]; then \
			PID=$$(cat $$PIDFILE); \
			NAME=$$(echo $$PIDFILE | sed 's|tmp/||;s|\.pid||'); \
			kill -TERM $$PID 2>/dev/null && echo "Stopped $$NAME (pid $$PID)" || echo "$$NAME not running (pid $$PID)"; \
			rm -f $$PIDFILE; \
		fi; \
	done

demo: web cli ## Start QNTX in demo mode with canvas export enabled
	@BACKEND_PORT=$${BACKEND_PORT:-8770}; \
	FRONTEND_PORT=$${FRONTEND_PORT:-8820}; \
	echo "📋 Starting demo canvas environment..."; \
	echo "  Backend:  http://localhost:$$BACKEND_PORT"; \
	echo "  Frontend: http://localhost:$$FRONTEND_PORT (with live reload)"; \
	echo "  Database: demo.db (persistent demo state)"; \
	echo "  Features: Canvas export enabled"; \
	echo ""; \
	lsof -ti:$$BACKEND_PORT | xargs kill -9 2>/dev/null || true; \
	lsof -ti:$$FRONTEND_PORT | xargs kill -9 2>/dev/null || true; \
	trap "echo ''; echo 'Shutting down demo servers...'; \
		test -n \"\$$BACKEND_PID\" && kill -TERM -\$$BACKEND_PID 2>/dev/null || true; \
		test -n \"\$$FRONTEND_PID\" && kill -TERM -\$$FRONTEND_PID 2>/dev/null || true; \
		sleep 1; \
		test -n \"\$$BACKEND_PID\" && kill -9 -\$$BACKEND_PID 2>/dev/null || true; \
		test -n \"\$$FRONTEND_PID\" && kill -9 -\$$FRONTEND_PID 2>/dev/null || true; \
		echo '✓ Demo servers stopped'" EXIT INT TERM; \
	set -m; \
	QNTX_DEMO=1 ./bin/qntx server --dev --no-browser --db-path demo.db -vvv & \
	BACKEND_PID=$$!; \
	cd web && VITE_QNTX_DEMO=1 bun run dev & \
	FRONTEND_PID=$$!; \
	echo "✨ Demo environment running"; \
	echo "Press Ctrl+C to stop"; \
	wait

dev-mobile: web cli ## Start dev servers and run iOS app in simulator
	@echo "📱 Starting mobile development environment..."
	@echo "  Backend:  http://localhost:$${BACKEND_PORT:-8770}"
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

test-ocaml: ## Run OCaml plugin tests (loom, kern)
	@echo "Running OCaml tests..."
	@cd qntx-plugins/loom && opam exec -- dune runtest
	@cd qntx-plugins/kern && opam exec -- dune runtest
	@echo "✓ OCaml tests complete"

test-d: ## Run D plugin tests (ix-net)
	@echo "Running D tests..."
	@$(MAKE) -C qntx-plugins/ix-net test
	@echo "✓ D tests complete"

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
	@echo "  Backend will start as sidecar on port $${BACKEND_PORT:-8770}"
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

# restart-plugin NAME
# Tells running QNTX to kill and relaunch a plugin. Silent no-op if QNTX isn't running.
define restart-plugin
	@TOML_PORT=$$(grep -E '^port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	 PORT=$${BACKEND_PORT:-$${TOML_PORT:-8770}}; \
	 curl -sf -X POST http://127.0.0.1:$$PORT/api/plugins/$(1)/restart > /dev/null 2>&1 || { \
		echo "  ⊘ QNTX not running — start QNTX to pick up new binary"; exit 0; }; \
	 echo "  ↻ RESTARTING $(1)..."; \
	 for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
		STATE=$$(curl -sf http://127.0.0.1:$$PORT/api/plugins 2>/dev/null \
			| python3 -c "import sys,json; plugins=json.load(sys.stdin).get('plugins',[]); p=[x for x in plugins if x.get('name')=='$(1)']; print(p[0].get('state','') if p else '')" 2>/dev/null); \
		if [ "$$STATE" = "running" ]; then \
			VERSION=$$(curl -sf http://127.0.0.1:$$PORT/api/plugins 2>/dev/null \
				| python3 -c "import sys,json; plugins=json.load(sys.stdin).get('plugins',[]); p=[x for x in plugins if x.get('name')=='$(1)']; print(p[0].get('version','') if p else '')" 2>/dev/null); \
			echo "  ✓ LOADED — $(1) $$VERSION live at http://127.0.0.1:$$PORT"; exit 0; \
		fi; \
		sleep 1; \
	 done; \
	 echo "  ✗ $(1) did not reach running state within 20s"
endef

# check-plugin-version DIR EXT VERSION_FILE
# Fails the build if source files changed but version file didn't.
# Usage: @$(call check-plugin-version,qntx-plugins/loom,.ml,qntx-plugins/loom/lib/version.ml)
define check-plugin-version
	@git diff --name-only HEAD -- $(1)/ | grep -q '\.$(2)$$' && \
	 ! git diff --name-only HEAD -- $(3) | grep -q . && \
	 echo "" && \
	 echo "  Impossible to debug or develop if we don't know what version is running." && \
	 echo "  You did not modify the version of this plugin in order to differentiate it." && \
	 echo "  Do at least a patch or debug bump to $(3)" && \
	 echo "" && \
	 exit 1 || true
endef

# TODO: each plugin should have their own ci, i think this Makefile should have the focus on QNTX only.

atproto-plugin: ## Build, install, and restart AT Protocol plugin
	$(call check-plugin-version,qntx-plugins/qntx-atproto,go,qntx-plugins/qntx-atproto/plugin.go)
	@$(MAKE) -C qntx-plugins/qntx-atproto install PREFIX=$(PREFIX)
	$(call restart-plugin,atproto)

github-plugin: ## Build, install, and restart GitHub plugin
	$(call check-plugin-version,qntx-plugins/qntx-github,go,qntx-plugins/qntx-github/plugin.go)
	@$(MAKE) -C qntx-plugins/qntx-github install PREFIX=$(PREFIX)
	$(call restart-plugin,github)

ix-json-plugin: ## Build, install, and restart ix-json plugin
	@$(MAKE) -C qntx-plugins/ix-json install PREFIX=$(PREFIX)
	$(call restart-plugin,ix-json)

ix-bin-plugin: ## Build, install, and restart ix-bin D plugin
	$(call check-plugin-version,qntx-plugins/ix-bin,d,qntx-plugins/ix-bin/source/ixbin/version_.d)
	@$(MAKE) -C qntx-plugins/ix-bin install PREFIX=$(PREFIX)
	$(call restart-plugin,ix-bin)

ix-net-plugin: ## Build, install, and restart ix-net D plugin
	$(call check-plugin-version,qntx-plugins/ix-net,d,qntx-plugins/ix-net/source/ixnet/version_.d)
	@$(MAKE) -C qntx-plugins/ix-net install PREFIX=$(PREFIX)
	$(call restart-plugin,ix-net)

faal-plugin: ## Build, install, and restart faal chaos testing D plugin
	$(call check-plugin-version,qntx-plugins/faal,d,qntx-plugins/faal/source/faal/version_.d)
	@$(MAKE) -C qntx-plugins/faal install PREFIX=$(PREFIX)
	$(call restart-plugin,faal)

openrouter-plugin: ## Build, install, and restart OpenRouter plugin
	$(call check-plugin-version,qntx-plugins/qntx-openrouter,go,qntx-plugins/qntx-openrouter/plugin.go)
	@$(MAKE) -C qntx-plugins/qntx-openrouter install PREFIX=$(PREFIX)
	$(call restart-plugin,openrouter)

pty-glyph-plugin: ## Build, install, and restart pty-glyph plugin
	$(call check-plugin-version,qntx-plugins/pty-glyph,rs,qntx-plugins/pty-glyph/Cargo.toml)
	@$(MAKE) -C qntx-plugins/pty-glyph install PREFIX=$(PREFIX)
	$(call restart-plugin,pty-glyph)

meili-plugin: ## Build, install, and restart MeiliSearch plugin
	$(call check-plugin-version,qntx-plugins/qntx-meili,rs,qntx-plugins/qntx-meili/Cargo.toml)
	@$(MAKE) -C qntx-plugins/qntx-meili install PREFIX=$(PREFIX)
	$(call restart-plugin,meili)

loom-plugin: ## Build, install, and restart loom plugin (OCaml)
	$(call check-plugin-version,qntx-plugins/loom,ml,qntx-plugins/loom/lib/version.ml)
	@$(MAKE) -C qntx-plugins/loom install PREFIX=$(PREFIX)
	$(call restart-plugin,loom)

kern-plugin: ## Build, install, and restart kern plugin (OCaml Ax parser)
	$(call check-plugin-version,qntx-plugins/kern,ml,qntx-plugins/kern/lib/version.ml)
	@$(MAKE) -C qntx-plugins/kern install PREFIX=$(PREFIX)
	$(call restart-plugin,kern)

llama-cpp-plugin: ## Build, install, and restart llama-cpp plugin (C++ local LLM)
	$(call check-plugin-version,qntx-plugins/llama-cpp,cpp,qntx-plugins/llama-cpp/src/plugin.h)
	$(call check-plugin-version,qntx-plugins/llama-cpp,h,qntx-plugins/llama-cpp/src/plugin.h)
	@$(MAKE) -C qntx-plugins/llama-cpp install PREFIX=$(PREFIX)
	$(call restart-plugin,llama-cpp)


rust-sqlite: ## Build Rust SQLite storage library with FFI support (for CGO integration)
	@echo "Building Rust SQLite storage library..."
	$(call ground-notify,rust-build,Rust: building qntx-sqlite)
	@cargo build --release --package qntx-sqlite --features ffi --lib || { \
		if [ -f "$(GROUND_DB)" ]; then sqlite3 "$(GROUND_DB)" "INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes) VALUES ('make-rust-build-failed-$$(date +%s)', '[\"qntx\"]', '[\"immediate:rust-build-failed\"]', '[\"project:teranos/QNTX\"]', '[\"make\"]', '$$(date -u +%Y-%m-%dT%H:%M:%SZ)', 'make', '{\"detail\":\"Rust: qntx-sqlite build FAILED\",\"after\":0}')"; fi; \
		exit 1; }
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


# TODO: move to its own plugin Makefile:
# Rust Reduce plugin (PyO3-based UMAP dimensionality reduction)
# REQUIRES Nix: Python linking + umap-learn dependency
rust-reduce: ## Build and install Rust Reduce plugin to ~/.qntx/plugins/
	@echo "Building qntx-reduce-plugin via Nix..."
	@nix build ./qntx-plugins/qntx-reduce#qntx-reduce-plugin
	@mkdir -p bin $(PREFIX)/plugins
	@rm -f bin/qntx-reduce-plugin $(PREFIX)/plugins/qntx-reduce-plugin
	@cp -L result/bin/qntx-reduce-plugin bin/
	@chmod +x bin/qntx-reduce-plugin
	@cp bin/qntx-reduce-plugin $(PREFIX)/plugins/
	@chmod +x $(PREFIX)/plugins/qntx-reduce-plugin
	@echo "✓ qntx-reduce-plugin built and installed to $(PREFIX)/plugins/"
