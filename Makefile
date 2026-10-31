.PHONY: cli web-css test test-suite test-parquet test-postgres test-coverage test-verbose clean server dev install proto rust-sqlite ats laye wasm-tarballs parity says openapi quickdev publish-crates

# The frontend is teranos/QNTX-Web. web/ here is the CSS QNTX itself reads (web/README.md).
QNTX_WEB ?= ../QNTX-Web

# Installation prefix (override with PREFIX=/custom/path make install)
PREFIX ?= $(HOME)/.qntx

# Use prebuilt qntx if available in PATH, otherwise use ./bin/qntx
QNTX := $(shell command -v qntx 2>/dev/null || echo ./bin/qntx)

# Ground immediate delivery — notify the Claude session that runs this make of build progress.
# Usage: @$(call ground-notify,name,detail message)
# The notice is addressed to the session make runs in (CLAUDE_CODE_SESSION_ID): ground
# reads a session address before a project one, and a project address reaches every
# session with that project as its cwd, which is not the one that built when make
# runs in a checkout elsewhere. Without a session, the notice goes to nobody.
GROUND_DB := $(HOME)/.local/share/ground/ground.db
define ground-notify
	@if [ -f "$(GROUND_DB)" ] && [ -n "$$CLAUDE_CODE_SESSION_ID" ]; then \
		TS=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
		sqlite3 "$(GROUND_DB)" "INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes) VALUES ('make-$(1)-' || strftime('%s','now'), '[\"qntx\"]', '[\"immediate:$(1)\"]', '[\"session:$$CLAUDE_CODE_SESSION_ID\"]', '[\"make\"]', '$$TS', 'make', json_object('detail', '$(2) at ' || time('now','localtime'), 'after', 0))"; \
	fi
endef

# docs/release.md
VERSION_TAG := $(shell git describe --tags --match 'v*' --dirty 2>/dev/null || echo dev)
GO_LDFLAGS = -ldflags="-X 'github.com/teranos/QNTX/internal/version.VersionTag=$(VERSION_TAG)' -X 'github.com/teranos/QNTX/internal/version.BuildTime=$(shell date -u '+%Y-%m-%d %H:%M:%S UTC')' -X 'github.com/teranos/QNTX/internal/version.CommitHash=$(shell git rev-parse HEAD)'"

BUILD_TAGS := rustsqlite,qntxwasm

cli: rust-sqlite ats ## Build QNTX CLI binary (with Rust optimizations and WASM parser)
	@echo "Building QNTX CLI with Rust optimizations (sqlite) and WASM (parser, fuzzy)..."
	$(call ground-notify,go-build,Go: building qntx cli)
	@# cargo writes libats_sqlite as a shared library beside the static one, and
	@# the linker takes the shared one, at this tree's absolute path: the darwin
	@# release loaded on no machine but the runner that built it. Gone, the
	@# binary carries the static library, as a plugin's does.
	@rm -f target/release/libats_sqlite.dylib target/release/deps/libats_sqlite.dylib target/release/libats_sqlite.so target/release/deps/libats_sqlite.so
	@go build -tags "$(BUILD_TAGS)" $(GO_LDFLAGS) -o bin/qntx ./cmd/qntx || { \
		if [ -f "$(GROUND_DB)" ]; then sqlite3 "$(GROUND_DB)" "INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes) VALUES ('make-go-build-failed-$$(date +%s)', '[\"qntx\"]', '[\"immediate:go-build-failed\"]', '[\"project:teranos/QNTX\"]', '[\"make\"]', '$$(date -u +%Y-%m-%dT%H:%M:%SZ)', 'make', '{\"detail\":\"Go: qntx cli build FAILED\",\"after\":0}')"; fi; \
		exit 1; }

# QuickDev: "One of the distributions of QNTX, purpose is to build FAST, help
# with plugin development, for use by Coding Agent Web and Mobile sessions.
# Doesn't need ATS or wasm"
# Nor the Tablet of Reach: on loopback without auth every caller is ROOT, so
# there it is "just friction without reward".
quickdev: ## Build QuickDev, as bin/qntx-quickdev: no ATS, no WASM, no Tablet of Reach, no auth, loopback only
	@go build -tags quickdev $(GO_LDFLAGS) -o bin/qntx-quickdev ./cmd/qntx

openapi: ## Write what the node serves, from the reach table and the handlers' own prose
	@go run ./cmd/openapi

# "Make parity would just be for the storage backend specifically": a signum
# held to a reference it follows is the parity sigil's (server/parity).
parity: rust-sqlite ## Report whether SQLite, DuckDB and Postgres each hold every thing QNTX persists
	@nix build .#postgres --out-link result-postgres
	@nix develop .#default --command cargo build --release -p ats-duckdb --features ffi --lib
	@cargo build --release -p ats-postgres --features ffi --lib
	@scripts/with-postgres.sh result-postgres/bin nix develop .#default --command env LD_LIBRARY_PATH=$(CURDIR)/target/release DYLD_LIBRARY_PATH=$(CURDIR)/target/release go run -tags rustduckdb,rustpostgres ./cmd/parity

# The generated Go keeps no .proto comment, and the parity sigil gives what our
# fields say of themselves beside what each spec says of its own. Umami's API
# document is 1.4 MB and parity reads only its words, so nix fetches it by its
# hash and keeps those.
says: ## Write what the specs and our protocol say of themselves for the parity sigil
	@go run ./cmd/says
	@nix eval --raw -f nix/references/umami.nix > server/parity/umami_v3.3.1_ca661c7/openapi.words.json
	@nix eval --raw -f nix/references/a2a.nix > server/parity/a2a_v1.0.1_3303592/specification.md
	@nix eval --raw -f nix/references/github.nix | jq . > server/parity/github_2026-03-10_7bdf5f0/openapi.json

server: cli ## Start QNTX WebSocket server
	@echo "Starting QNTX server..."
	@./bin/qntx server

dev: cli ## Build the CLI and start the node in dev mode; the page is QNTX-Web's `bun run dev`
	$(call ground-notify,rebuilding,make dev: rebuilding QNTX)
	@TOML_BACKEND_PORT=$$(grep -E '^port\s*=' am.toml 2>/dev/null | head -1 | sed 's/.*=\s*//;s/[^0-9]//g' || echo ""); \
	BACKEND_PORT=$${BACKEND_PORT:-$${TOML_BACKEND_PORT:-8770}}; \
	echo "🚀 Starting development node..."; \
	echo "  Backend:  http://localhost:$$BACKEND_PORT"; \
	echo "  Frontend: cd $(QNTX_WEB) && QNTX_AM_TOML=$(CURDIR)/am.toml bun run dev"; \
	echo "  Database: Uses am.toml configuration"; \
	echo ""; \
	lsof -ti:$$BACKEND_PORT | xargs kill -9 2>/dev/null || true; \
	GOTRACEBACK=crash ./bin/qntx server --dev --no-browser -vvv 2> tmp/qntx-crash.log

web-css: ## Copy the CSS QNTX itself reads from a QNTX-Web checkout (web/README.md)
	@test -d $(QNTX_WEB)/css || { echo "no QNTX-Web at $(QNTX_WEB): set QNTX_WEB=<checkout>"; exit 1; }
	@for f in css/tokens.css css/canvas.css css/window.css css/core.css css/utilities.css css/docs.css css/element/title-bar.css css/element/states/window.css qntx.jpg; do \
		cp $(QNTX_WEB)/$$f web/$$f; \
	done
	@git status --short web/

# A verdict on both paths. Piping this to anything hands the reader the pipe's
# exit code, not make's, so the last line has to carry the answer by itself.
test: ## Run all tests (Go + parquet + postgres backends)
	@$(MAKE) --no-print-directory test-suite || { echo "✗ TESTS FAILED"; exit 1; }
	@echo "✓ All tests complete"

test-suite: ## The suite itself. Run `make test`, which reports a verdict.
	@go test -tags "rustsqlite,qntxwasm" -short ./...
	@$(MAKE) --no-print-directory test-parquet
	@$(MAKE) --no-print-directory test-postgres

# The parquet backend is behind `rustduckdb` and dynamically links Nix's
# libduckdb, so it cannot ride along in the default tags without making every
# build require the dev shell. Run separately instead of not at all: ADR-024's
# CI floor is against file://, and until that lands this is the only thing
# exercising the FFI at all.
test-parquet: ## Run parquet backend tests (requires Nix for libduckdb)
	@command -v nix >/dev/null 2>&1 || { echo "  ⊘ nix not found — parquet backend tests skipped"; exit 0; }
	@nix develop .#default --command cargo build --release -p ats-duckdb --features ffi --lib
	@nix develop .#default --command cargo test -p ats-duckdb --lib --features ffi
	@nix develop .#default --command go test -tags "rustsqlite,qntxwasm,rustduckdb" -short ./ats/storage/duckdbcgo/... ./cmd/qntx/commands/ ./cmd/parity/
	@nix develop .#default --command go build -tags "rustsqlite,qntxwasm,rustduckdb" ./...

# The postgres backend runs against the pinned Supabase Postgres, a server of
# its own per run (scripts/with-postgres.sh).
test-postgres: rust-sqlite ## Run postgres backend tests against the pinned Supabase Postgres (requires Nix)
	@command -v nix >/dev/null 2>&1 || { echo "  ⊘ nix not found — postgres backend tests skipped"; exit 0; }
	@nix build .#postgres --out-link result-postgres
	@nix develop .#default --command cargo build --release -p ats-duckdb --features ffi --lib
	@cargo build --release -p ats-postgres --features ffi --lib
	@scripts/with-postgres.sh result-postgres/bin cargo test -p ats-postgres
	@scripts/with-postgres.sh result-postgres/bin nix develop .#default --command env LD_LIBRARY_PATH=$(CURDIR)/target/release DYLD_LIBRARY_PATH=$(CURDIR)/target/release go test -tags "rustsqlite,qntxwasm,rustduckdb,rustpostgres" -short ./ats/storage/postgrescgo/... ./cmd/qntx/commands/ ./cmd/parity/

test-coverage: ## Run Go tests with coverage
	@echo "Running Go tests with coverage..."
	@mkdir -p tmp
	@# Test with core tags to ensure we test what we ship
	@go test -tags "rustsqlite,qntxwasm" -short -coverprofile=tmp/coverage.out -covermode=count ./...
	@go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "✓ Go tests complete. Coverage report: tmp/coverage.html"

test-verbose: ## Run Go tests with verbose output and coverage
	@echo "Running Go tests with verbose output..."
	@mkdir -p tmp
	@go test -v -coverprofile=tmp/coverage.out -covermode=count ./...
	@go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "✓ Go tests complete. Coverage report: tmp/coverage.html"

clean: ## Clean build artifacts
	@rm -rf crates/ats-wasm/pkg crates/laye-p2p/pkg tmp/wasm

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

# Plugins are their own repositories, on a node through their record (ADR-002).
# "each plugin needs to move into their own respective repo as well"

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
	@ls -lh crates/ats-wasm/pkg/*.wasm 2>/dev/null | awk '{print "  ✓ " $$9 " " $$5}' || (echo "    ERROR: wasm-pack ran but produced no .wasm files"; exit 1)

# Built from crates/laye-p2p, not fetched. The hash-pinning this replaces
# existed only because the artifact came from a deploy we did not build.
laye: ## Build laye-p2p as browser WASM into crates/laye-p2p/pkg/
	@echo "Building laye-p2p browser WASM..."
	@if ! command -v wasm-pack >/dev/null 2>&1; then \
		echo "  ⚠️  wasm-pack not found. Install with: cargo install wasm-pack"; \
		exit 1; \
	fi
	@cd crates/laye-p2p && wasm-pack build --target web
	@ls -lh crates/laye-p2p/pkg/laye_p2p_bg.wasm | awk '{print "  ✓ laye_p2p_bg.wasm " $$5}'

# QNTX-Web depends on these two by the release they are attached to:
# "QNTX-Web needs to be easy to be developed on, so need to be a way to develop on it without needing to build ATS or Laye"
wasm-tarballs: ats laye ## Pack the browser WASM as the two tarballs a release attaches, into tmp/wasm/
	@mkdir -p tmp/wasm
	@cd crates/ats-wasm/pkg && bun pm pack --quiet --destination $(CURDIR)/tmp/wasm
	@cd crates/laye-p2p/pkg && bun pm pack --quiet --destination $(CURDIR)/tmp/wasm
	@ls -lh tmp/wasm/*.tgz


# TODO: move to its own plugin Makefile:
publish-crates: ## Stamp the tag into Cargo.toml and publish qntx-ats, qntx-proto, qntx-grpc
	@./scripts/stamp-crate-version.sh
	@cargo publish -p qntx-ats
	@cargo publish -p qntx-proto
	@cargo publish -p qntx-grpc
