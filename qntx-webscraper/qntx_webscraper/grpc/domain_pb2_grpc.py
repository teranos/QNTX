"""Generated gRPC stubs for DomainPlugin.

NOTE: This is a placeholder. Run `make generate` to create the actual stubs from
the QNTX proto files at ../plugin/grpc/protocol/domain.proto
"""


class DomainPluginServiceServicer:
    """Placeholder for generated DomainPluginService servicer base class."""

    def Metadata(self, request, context):
        raise NotImplementedError()

    def Initialize(self, request, context):
        raise NotImplementedError()

    def Shutdown(self, request, context):
        raise NotImplementedError()

    def HandleHTTP(self, request, context):
        raise NotImplementedError()

    def HandleWebSocket(self, request_iterator, context):
        raise NotImplementedError()

    def Health(self, request, context):
        raise NotImplementedError()


def add_DomainPluginServiceServicer_to_server(servicer, server):
    """Placeholder for generated add_*_to_server function."""
    raise NotImplementedError("Run 'make generate' to create real gRPC stubs")
