# QNTX Python Types

Auto-generated Python type definitions from QNTX's Go source code.

## Installation

**Using uv (recommended):**
```bash
uv pip install -e types/generated/python
# Or add to your project:
uv add ./types/generated/python
```

**Using pip:**
```bash
pip install -e types/generated/python
```

## Usage

```python
from qntx_types import Job, JobStatus, Progress
from qntx_types.sym import PULSE, COMMAND_SYMBOLS

# Create a job instance
job = Job(
    id="123",
    handler_name="test",
    status="running",
)

# Access symbol constants
print(f"Pulse symbol: {PULSE}")
```

## Generated Files

- [`__init__.py`](./__init__.py) - Package entry point with re-exports
- [`graph.py`](./graph.py) - graph types
- [`sym.py`](./sym.py) - Symbol constants and mappings

## Type Compatibility

All types are Python `@dataclass` decorated classes compatible with:

- JSON serialization via `dataclasses.asdict()`
- Type checking with mypy/pyright
- IDE autocomplete and documentation

### Example: JSON Serialization

```python
import json
from dataclasses import asdict
from qntx_types import Job

job = Job(id="123", handler_name="test", status="completed")
json_str = json.dumps(asdict(job))
```

## Regeneration

Types are regenerated with:

```bash
make types
# or
./qntx typegen --lang python --output types/generated/
```

**Do not manually edit** - changes will be overwritten when types are regenerated.
