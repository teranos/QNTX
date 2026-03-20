.PHONY: build install clean test

PREFIX ?= $(HOME)/.qntx
REBAR3 ?= rebar3

build: ## Build sentry plugin escript
	@echo "Building qntx-sentry-plugin (Erlang/OTP)..."
	@$(REBAR3) escriptize
	@mkdir -p bin
	@cp _build/default/bin/qntx-sentry-plugin bin/qntx-sentry-plugin
	@chmod +x bin/qntx-sentry-plugin
	@echo "✓ bin/qntx-sentry-plugin"

test: ## Run Erlang tests
	@echo "Running sentry tests..."
	@$(REBAR3) eunit
	@echo "✓ sentry tests passed"

install: build ## Install sentry plugin to ~/.qntx/plugins/
	@echo "Installing qntx-sentry-plugin to $(PREFIX)/plugins..."
	@mkdir -p $(PREFIX)/plugins
	@cp bin/qntx-sentry-plugin $(PREFIX)/plugins/qntx-sentry-plugin
	@chmod +x $(PREFIX)/plugins/qntx-sentry-plugin
	@echo "✓ qntx-sentry-plugin → $(PREFIX)/plugins/qntx-sentry-plugin"

clean: ## Clean build artifacts
	@rm -rf _build/ bin/
