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

test-unit:
	uv run pytest tests/unit -v --cov=qntx_webscraper

test-integration:
	uv run python test_integration.py

test-security:
	uv run python tests/security/test_ssrf.py

lint:
	uv run ruff check .
	uv run mypy qntx_webscraper --ignore-missing-imports

format:
	uv run black .
	uv run isort .

security-scan:
	uv run bandit -r qntx_webscraper
	uv run safety check

# Clean generated files
clean:
	rm -rf qntx_webscraper/grpc/*_pb2*.py
	rm -rf __pycache__ .pytest_cache
	rm -rf *.egg-info
