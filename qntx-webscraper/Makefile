.PHONY: setup generate run clean

# Setup the project with uv
setup:
	uv venv
	uv pip install -e ".[dev]"

# Generate gRPC stubs from proto files
generate:
	./generate_grpc.sh

# Run the plugin
run:
	uv run qntx-webscraper --port 9001

# Run with debug logging
run-debug:
	uv run qntx-webscraper --port 9001 --log-level DEBUG

# Run tests
test:
	uv run pytest

# Clean generated files
clean:
	rm -rf qntx_webscraper/grpc/*_pb2*.py
	rm -rf __pycache__ .pytest_cache
	rm -rf *.egg-info
