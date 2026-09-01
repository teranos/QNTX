.PHONY: cli web run-web lint sacred-error test-web test-jsdom test test-suite test-parquet test-ocaml test-d test-coverage test-verbose clean server dev install proto code-plugin atproto-plugin github-plugin ix-json-plugin ix-bin-plugin ix-net-plugin faal-plugin pty-glyph-plugin loom-plugin kern-plugin llama-cpp-plugin meili-plugin rust-sqlite ats laye rust-reduce parity

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

cli: rust-sqlite ats ## Build QNTX CLI binary (with Rust optimizations and WASM parser)
	@echo "Building QNTX CLI with Rust optimizations (sqlite) and WASM (parser, fuzzy)..."
	$(call ground-notify,go-build,Go: building qntx cli)
	@go build -tags "$(BUILD_TAGS)" -ldflags="-X 'github.com/teranos/QNTX/internal/version.VersionTag=$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)' -X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'" -o bin/qntx ./cmd/qntx || { \
		if [ -f "$(GROUND_DB)" ]; then sqlite3 "$(GROUND_DB)" "INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes) VALUES ('make-go-build-failed-$$(date +%s)', '[\"qntx\"]', '[\"immediate:go-build-failed\"]', '[\"project:teranos/QNTX\"]', '[\"make\"]', '$$(date -u +%Y-%m-%dT%H:%M:%SZ)', 'make', '{\"detail\":\"Go: qntx cli build FAILED\",\"after\":0}')"; fi; \
		exit 1; }

parity: ## Report which persisted state each storage backend has (ADR-024 gap)
	@go run ./cmd/parity

# git is the baseline, so there is no file to keep in step. What already stands
# keeps standing; what this branch added is what answers. Exit 2 and not 1: a
# ground rite that declares no catch is given 1, which means "not yet".
# The Go half was the whole of it, so a red Rust tree shipped while CI was
# still going red about it. Both halves, one gate.
sacred-error: ## Fail on any dropped failure this branch adds (.golangci.yml, clippy)
	@command -v nix >/dev/null 2>&1 || { echo "sacred-error needs nix: the linters are pinned in flake.nix" >&2; exit 1; }
	@# One shell for all three: entering it costs ~55s and the linting itself
	@# costs five, so three entries would be two minutes of flake evaluation.
	@# The clippy exclusions are the ones .github/workflows/rs.yml names —
	@# ats-duckdb needs libduckdb, qntx-reduce-plugin builds only through Nix.
	@nix develop .#default --command bash -c '\
		set -e; \
		golangci-lint run --issues-exit-code 2 --new-from-merge-base origin/main ./...; \
		export RUSTFLAGS=-Dwarnings; \
		cargo clippy --workspace --exclude ats-duckdb --exclude qntx-reduce-plugin --all-targets || exit 2; \
		cargo clippy --package ats-duckdb --all-targets || exit 2'

server: cli ## Start QNTX WebSocket server
	@echo "Starting QNTX server..."
	@./bin/qntx server

dev: ## Build frontend and CLI, then start development servers (backend + frontend with live reload)
	$(call ground-notify,rebuilding,make dev: rebuilding QNTX)
	@$(MAKE) web cli
	@# Read ports from am.toml if exists, otherwise use defaults
	@TOML_BACKEND_PORT=$$(grep -E '^port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	TOML_FRONTEND_PORT=$$(grep -E '^frontend_port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	BACKEND_PORT=$${BACKEND_PORT:-$${TOML_BACKEND_PORT:-8770}}; \
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

web: ats laye ## Build web assets with Bun (requires WASM)
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

lint: ## Check the frontend bans (web/eslint.config.js)
	@if [ ! -d "web/node_modules" ]; then \
		cd web && bun install; \
	fi
	@cd web && bun run lint

# A verdict on both paths. Piping this to anything hands the reader the pipe's
# exit code, not make's, so the last line has to carry the answer by itself.
test: lint ## Run all tests (Go + TypeScript + parquet backend)
	@$(MAKE) --no-print-directory test-suite || { echo "✗ TESTS FAILED"; exit 1; }
	@echo "✓ All tests complete"

test-suite: ## The suite itself. Run `make test`, which reports a verdict.
	@go test -tags "rustsqlite,qntxwasm" -short ./...
	@$(MAKE) --no-print-directory test-parquet
	@if [ ! -d "web/node_modules" ]; then \
		cd web && bun install; \
	fi
	@cd web && USE_JSDOM=1 bun test

# The parquet backend is behind `rustduckdb` and dynamically links Nix's
# libduckdb, so it cannot ride along in the default tags without making every
# build require the dev shell. Run separately instead of not at all: ADR-024's
# CI floor is against file://, and until that lands this is the only thing
# exercising the FFI at all.
test-parquet: ## Run parquet backend tests (requires Nix for libduckdb)
	@command -v nix >/dev/null 2>&1 || { echo "  ⊘ nix not found — parquet backend tests skipped"; exit 0; }
	@nix develop .#default --command cargo build --release -p ats-duckdb --features ffi --lib
	@nix develop .#default --command cargo test -p ats-duckdb --lib --features ffi
	@nix develop .#default --command go test -tags "rustsqlite,qntxwasm,rustduckdb" -short ./ats/storage/duckdbcgo/...
	@nix develop .#default --command go build -tags "rustsqlite,qntxwasm,rustduckdb" ./...

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
	$(call ground-notify,rust-build,Rust: building ats-sqlite)
	@cargo build --release --package ats-sqlite --features ffi --lib || { \
		if [ -f "$(GROUND_DB)" ]; then sqlite3 "$(GROUND_DB)" "INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes) VALUES ('make-rust-build-failed-$$(date +%s)', '[\"qntx\"]', '[\"immediate:rust-build-failed\"]', '[\"project:teranos/QNTX\"]', '[\"make\"]', '$$(date -u +%Y-%m-%dT%H:%M:%SZ)', 'make', '{\"detail\":\"Rust: ats-sqlite build FAILED\",\"after\":0}')"; fi; \
		exit 1; }
	@echo "✓ libats_sqlite built in target/release/"
	@echo "  Static:  libats_sqlite.a"
	@echo "  Shared:  libats_sqlite.so (Linux) / libats_sqlite.dylib (macOS)"

ats: ## Build ats as WASM module (for wazero integration + browser)
	@echo "Building ats WASM modules..."
	@echo "  [1/2] Building Go/wazero WASM..."
	@cargo build --release --target wasm32-unknown-unknown --package ats-wasm
	@cp target/wasm32-unknown-unknown/release/ats_wasm.wasm ats/wasm/ats.wasm
	@echo "  ✓ ats.wasm built and copied to ats/wasm/"
	@ls -lh ats/wasm/ats.wasm | awk '{print "    Size: " $$5}'
	@echo "  [2/2] Building browser WASM with wasm-bindgen..."
	@if ! command -v wasm-pack >/dev/null 2>&1; then \
		echo "  ⚠️  wasm-pack not found. Install with: cargo install wasm-pack"; \
		exit 1; \
	fi
	@cd crates/ats-wasm && wasm-pack build --target web --features browser
	@cp -r crates/ats-wasm/pkg/* web/wasm/
	@echo "  ✓ Browser WASM built and copied to web/wasm/"
	@ls -lh web/wasm/*.wasm 2>/dev/null | awk '{print "    Size: " $$5 " - " $$9}' || (echo "    ERROR: wasm-pack ran but produced no .wasm files"; exit 1)

# Built from crates/laye-p2p, not fetched. The hash-pinning this replaces
# existed only because the artifact came from a deploy we did not build.
laye: ## Build laye-p2p as browser WASM into web/wasm/
	@echo "Building laye-p2p browser WASM..."
	@if ! command -v wasm-pack >/dev/null 2>&1; then \
		echo "  ⚠️  wasm-pack not found. Install with: cargo install wasm-pack"; \
		exit 1; \
	fi
	@cd crates/laye-p2p && wasm-pack build --target web
	@cp crates/laye-p2p/pkg/laye_p2p.js crates/laye-p2p/pkg/laye_p2p.d.ts \
		crates/laye-p2p/pkg/laye_p2p_bg.wasm crates/laye-p2p/pkg/laye_p2p_bg.wasm.d.ts \
		web/wasm/
	@ls -lh web/wasm/laye_p2p_bg.wasm | awk '{print "  ✓ laye_p2p_bg.wasm " $$5}'


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
