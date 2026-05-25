# Example QNTX Python handler script
#
# Register with: ./attest.fish example.py
# Runtime: qntx-python-plugin (PyO3)
#
# The runtime provides:
#   - @watch(predicate, context=...) — subscribe to attestation predicates
#   - upstream — the triggering attestation payload (dict or None)
#   - attest() — create downstream attestations

@watch('data:incoming', context='my/ctx')
def process(upstream):
    """Called when a data:incoming attestation fires in my/ctx."""
    import json

    payload = json.loads(upstream) if isinstance(upstream, str) else upstream
    attrs = payload.get('attributes', {})
    subjects = payload.get('subjects', [])
    subject = subjects[0] if subjects else 'unknown'

    # Do work
    result = len(attrs)

    # Attest downstream
    attest(
        subjects=[subject],
        predicates=['data:processed'],
        contexts=['my/ctx'],
        attributes={
            'input_attrs': result,
            'status': 'done',
        },
    )
