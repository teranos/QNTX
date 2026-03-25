.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx
BUILD_DIR = .build/release

build: ## Build qntx-swift-metal-plugin
	@echo "Building qntx-swift-metal-plugin..."
	@swift build -c release
	@echo "✓ $(BUILD_DIR)/qntx-swift-metal-plugin"

install: build ## Install to ~/.qntx/plugins/
	@mkdir -p $(PREFIX)/plugins
	@cp $(BUILD_DIR)/qntx-swift-metal-plugin $(PREFIX)/plugins/qntx-swift-metal-plugin
	@chmod +x $(PREFIX)/plugins/qntx-swift-metal-plugin
	@echo "✓ qntx-swift-metal-plugin → $(PREFIX)/plugins/qntx-swift-metal-plugin"

clean: ## Clean build artifacts
	@swift package clean
	@echo "✓ Clean"
