/// ATSStore gRPC client — writes attestations to QNTX core.
///
/// Uses the gRPC callback service provided during plugin initialization.
/// Auth token from InitializeRequest is included in every RPC.
module ixotlp.ats;

import ixotlp.grpc;
import ixotlp.proto;
import ixotlp.log;

struct ATSClient {
    GrpcClient client;
    string authToken;
    bool connected;
    string endpoint;

    /// Connect to the ATSStore service endpoint.
    bool connect(string ep, string token) {
        endpoint = ep;
        authToken = token;
        connected = client.connect(ep);
        if (!connected) {
            logError("[ix-otlp] ATSStore: failed to connect to %s", ep);
        } else {
            logInfo("[ix-otlp] ATSStore: connected to %s", ep);
        }
        return connected;
    }

    /// Create an attestation using GenerateAndCreateAttestation.
    /// Returns true on success, sets error string on failure.
    bool createAttestation(ref const AttestationCommand cmd, ref string error) {
        if (!connected) {
            error = "ATSStore client not connected to " ~ endpoint;
            logError("[ix-otlp] ATSStore: createAttestation failed — not connected to %s", endpoint);
            return false;
        }

        auto requestBytes = encodeGenerateAttestationRequest(authToken, cmd);
        auto responseBytes = client.call(
            "/protocol.ATSStoreService/GenerateAndCreateAttestation",
            requestBytes
        );

        if (responseBytes.length == 0) {
            error = "empty response from ATSStore at " ~ endpoint;
            logError("[ix-otlp] ATSStore: empty response from %s", endpoint);
            return false;
        }

        auto resp = decodeGenerateAttestationResponse(responseBytes);
        if (!resp.success) {
            error = resp.error;
            logError("[ix-otlp] ATSStore: attestation rejected: %s", resp.error);
            return false;
        }
        return true;
    }

    void close() {
        client.close();
        connected = false;
    }
}
