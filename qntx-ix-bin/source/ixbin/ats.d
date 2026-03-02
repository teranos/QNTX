/// ATSStore gRPC client — writes attestations to QNTX core.
///
/// Uses the gRPC callback service provided during plugin initialization.
/// Auth token from InitializeRequest is included in every RPC.
module ixbin.ats;

import ixbin.grpc;
import ixbin.proto;

struct ATSClient {
    GrpcClient client;
    string authToken;
    bool connected;

    /// Connect to the ATSStore service endpoint.
    bool connect(string endpoint, string token) {
        authToken = token;
        connected = client.connect(endpoint);
        return connected;
    }

    /// Create an attestation using GenerateAndCreateAttestation.
    /// Returns true on success, sets error string on failure.
    bool createAttestation(ref const AttestationCommand cmd, ref string error) {
        if (!connected) {
            error = "ATSStore client not connected";
            return false;
        }

        auto requestBytes = encodeGenerateAttestationRequest(authToken, cmd);
        auto responseBytes = client.call(
            "/protocol.ATSStoreService/GenerateAndCreateAttestation",
            requestBytes
        );

        if (responseBytes.length == 0) {
            error = "empty response from ATSStore";
            return false;
        }

        auto resp = decodeGenerateAttestationResponse(responseBytes);
        if (!resp.success) {
            error = resp.error;
            return false;
        }
        return true;
    }

    void close() {
        client.close();
        connected = false;
    }
}
