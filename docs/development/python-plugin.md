# Python Plugin

The Python plugin enables executing Python code within QNTX. It runs as a separate Rust-based process using PyO3 for safe Python embedding.

## Executing Python Code

Python code can be executed via the `/execute` endpoint:

```json
POST /api/python/execute
{
  "code": "result = 1 + 2\nprint(f'Result: {result}')",
  "timeout_secs": 30
}
```

## Creating Attestations

The `attest()` function is available in Python code to create attestations:

```python
# Create an attestation
result = attest(
    subjects=["user:alice"],
    predicates=["completed"],
    contexts=["task:review-pr-123"]
)

print(f"Created attestation: {result['id']}")
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `subjects` | `list[str]` | Yes | Entity identifiers the attestation is about |
| `predicates` | `list[str]` | Yes | Actions or states being attested |
| `contexts` | `list[str]` | Yes | Contextual references (tasks, projects, etc.) |
| `actors` | `list[str]` | No | Who/what created this attestation |
| `attributes` | `dict` | No | Additional key-value metadata |

### Return Value

Returns a dictionary with the created attestation:

```python
{
    "id": "att_abc123",
    "subjects": ["user:alice"],
    "predicates": ["completed"],
    "contexts": ["task:review-pr-123"],
    "actors": ["python-plugin"],
    "timestamp": 1706234567,
    "source": "python"
}
```

### Example with Attributes

```python
result = attest(
    subjects=["service:api"],
    predicates=["health_check"],
    contexts=["env:production"],
    actors=["monitor:cron"],
    attributes={
        "latency_ms": 42,
        "status": "healthy"
    }
)
```

## Evaluating Expressions

For simple expressions, use `/evaluate`:

```json
POST /api/python/evaluate
{
  "expr": "2 ** 10"
}
```

Returns:
```json
{
  "success": true,
  "result": 1024
}
```

## Package Management (Planned)

Future package management will use `uv` for fast, deterministic Python dependencies:

```json
POST /api/python/uv/install
{
  "package": "requests"
}
```

Check module availability:

```json
GET /api/python/uv/check?module=numpy
```

**Note:** These endpoints are not yet implemented. Current builds include only bundled packages.
