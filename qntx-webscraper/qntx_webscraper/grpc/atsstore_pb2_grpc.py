"""Generated gRPC stubs for ATSStore.

NOTE: This is a placeholder. Run `make generate` to create the actual stubs from
the QNTX proto files at ../plugin/grpc/protocol/atsstore.proto
"""


class ATSStoreServiceStub:
    """Placeholder for generated ATSStoreService stub."""

    def __init__(self, channel):
        self.channel = channel

    def CreateAttestation(self, request):
        raise NotImplementedError("Run 'make generate' to create real gRPC stubs")

    def AttestationExists(self, request):
        raise NotImplementedError("Run 'make generate' to create real gRPC stubs")

    def GenerateAndCreateAttestation(self, request):
        raise NotImplementedError("Run 'make generate' to create real gRPC stubs")

    def GetAttestations(self, request):
        raise NotImplementedError("Run 'make generate' to create real gRPC stubs")
