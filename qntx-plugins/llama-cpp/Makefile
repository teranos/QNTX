.PHONY: build install clean

PREFIX ?= $(HOME)/.qntx
BUILD_DIR = build

build: ## Build qntx-llama-cpp-plugin
	@echo "Building qntx-llama-cpp-plugin..."
	@mkdir -p $(BUILD_DIR)
	@cd $(BUILD_DIR) && cmake .. -DCMAKE_BUILD_TYPE=Release
	@cd $(BUILD_DIR) && cmake --build . --parallel 3
	@echo "✓ $(BUILD_DIR)/qntx-llama-cpp-plugin"

install: build ## Install to ~/.qntx/plugins/
	@mkdir -p $(PREFIX)/plugins
	@cp $(BUILD_DIR)/qntx-llama-cpp-plugin $(PREFIX)/plugins/qntx-llama-cpp-plugin
	@chmod +x $(PREFIX)/plugins/qntx-llama-cpp-plugin
	@echo "✓ qntx-llama-cpp-plugin → $(PREFIX)/plugins/qntx-llama-cpp-plugin"

clean: ## Clean build artifacts
	@rm -rf $(BUILD_DIR)
	@echo "✓ Clean"
