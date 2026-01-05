"""Generated protocol buffer code for ATSStore.

NOTE: This is a placeholder. Run `make generate` to create the actual stubs from
the QNTX proto files at ../plugin/grpc/protocol/atsstore.proto
"""

# Placeholder classes - will be replaced by protoc-generated code


class Attestation:
    """Placeholder for generated Attestation message."""

    def __init__(self, **kwargs):
        self.id = kwargs.get("id", "")
        self.subjects = kwargs.get("subjects", [])
        self.predicates = kwargs.get("predicates", [])
        self.contexts = kwargs.get("contexts", [])
        self.actors = kwargs.get("actors", [])
        self.timestamp = kwargs.get("timestamp", 0)
        self.source = kwargs.get("source", "")
        self.attributes_json = kwargs.get("attributes_json", "")
        self.created_at = kwargs.get("created_at", 0)


class AttestationCommand:
    """Placeholder for generated AttestationCommand message."""

    def __init__(self, **kwargs):
        self.subjects = kwargs.get("subjects", [])
        self.predicates = kwargs.get("predicates", [])
        self.contexts = kwargs.get("contexts", [])
        self.actors = kwargs.get("actors", [])
        self.timestamp = kwargs.get("timestamp", 0)
        self.attributes_json = kwargs.get("attributes_json", "")


class AttestationFilter:
    """Placeholder for generated AttestationFilter message."""

    def __init__(self, **kwargs):
        self.subjects = kwargs.get("subjects", [])
        self.predicates = kwargs.get("predicates", [])
        self.contexts = kwargs.get("contexts", [])
        self.actors = kwargs.get("actors", [])
        self.time_start = kwargs.get("time_start", 0)
        self.time_end = kwargs.get("time_end", 0)
        self.limit = kwargs.get("limit", 0)


class CreateAttestationRequest:
    """Placeholder for generated CreateAttestationRequest message."""

    def __init__(self, **kwargs):
        self.auth_token = kwargs.get("auth_token", "")
        self.attestation = kwargs.get("attestation")


class CreateAttestationResponse:
    """Placeholder for generated CreateAttestationResponse message."""

    def __init__(self, **kwargs):
        self.success = kwargs.get("success", False)
        self.error = kwargs.get("error", "")


class AttestationExistsRequest:
    """Placeholder for generated AttestationExistsRequest message."""

    def __init__(self, **kwargs):
        self.auth_token = kwargs.get("auth_token", "")
        self.id = kwargs.get("id", "")


class AttestationExistsResponse:
    """Placeholder for generated AttestationExistsResponse message."""

    def __init__(self, **kwargs):
        self.exists = kwargs.get("exists", False)


class GenerateAttestationRequest:
    """Placeholder for generated GenerateAttestationRequest message."""

    def __init__(self, **kwargs):
        self.auth_token = kwargs.get("auth_token", "")
        self.command = kwargs.get("command")


class GenerateAttestationResponse:
    """Placeholder for generated GenerateAttestationResponse message."""

    def __init__(self, **kwargs):
        self.success = kwargs.get("success", False)
        self.error = kwargs.get("error", "")
        self.attestation = kwargs.get("attestation")


class GetAttestationsRequest:
    """Placeholder for generated GetAttestationsRequest message."""

    def __init__(self, **kwargs):
        self.auth_token = kwargs.get("auth_token", "")
        self.filter = kwargs.get("filter")


class GetAttestationsResponse:
    """Placeholder for generated GetAttestationsResponse message."""

    def __init__(self, **kwargs):
        self.success = kwargs.get("success", False)
        self.error = kwargs.get("error", "")
        self.attestations = kwargs.get("attestations", [])
