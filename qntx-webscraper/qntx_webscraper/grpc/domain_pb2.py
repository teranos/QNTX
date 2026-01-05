"""Generated protocol buffer code for DomainPlugin.

NOTE: This is a placeholder. Run `make generate` to create the actual stubs from
the QNTX proto files at ../plugin/grpc/protocol/domain.proto
"""


class Empty:
    """Placeholder for generated Empty message."""

    pass


class MetadataResponse:
    """Placeholder for generated MetadataResponse message."""

    def __init__(self, **kwargs):
        self.name = kwargs.get("name", "")
        self.version = kwargs.get("version", "")
        self.qntx_version = kwargs.get("qntx_version", "")
        self.description = kwargs.get("description", "")
        self.author = kwargs.get("author", "")
        self.license = kwargs.get("license", "")


class InitializeRequest:
    """Placeholder for generated InitializeRequest message."""

    def __init__(self, **kwargs):
        self.ats_store_endpoint = kwargs.get("ats_store_endpoint", "")
        self.queue_endpoint = kwargs.get("queue_endpoint", "")
        self.auth_token = kwargs.get("auth_token", "")
        self.config = kwargs.get("config", {})


class HTTPRequest:
    """Placeholder for generated HTTPRequest message."""

    def __init__(self, **kwargs):
        self.method = kwargs.get("method", "")
        self.path = kwargs.get("path", "")
        self.headers = kwargs.get("headers", [])
        self.body = kwargs.get("body", b"")


class HTTPResponse:
    """Placeholder for generated HTTPResponse message."""

    def __init__(self, **kwargs):
        self.status_code = kwargs.get("status_code", 200)
        self.headers = kwargs.get("headers", [])
        self.body = kwargs.get("body", b"")


class HTTPHeader:
    """Placeholder for generated HTTPHeader message."""

    def __init__(self, **kwargs):
        self.name = kwargs.get("name", "")
        self.values = kwargs.get("values", [])


class WebSocketMessage:
    """Placeholder for generated WebSocketMessage message."""

    CONNECT = 0
    DATA = 1
    CLOSE = 2
    PING = 3
    PONG = 4
    ERROR = 5

    def __init__(self, **kwargs):
        self.type = kwargs.get("type", 0)
        self.data = kwargs.get("data", b"")
        self.headers = kwargs.get("headers", {})
        self.timestamp = kwargs.get("timestamp", 0)


class HealthResponse:
    """Placeholder for generated HealthResponse message."""

    def __init__(self, **kwargs):
        self.healthy = kwargs.get("healthy", True)
        self.message = kwargs.get("message", "")
        self.details = kwargs.get("details", {})
