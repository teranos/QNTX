/// ATSStore gRPC client — writes attestations to QNTX core.
///
/// Uses the gRPC callback service provided during plugin initialization.
/// Auth token from InitializeRequest is included in every RPC.
module ixnet.ats;

import ixnet.grpc;
import ixnet.proto;
import ixnet.log;

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
            logError("[ix-net] ATSStore: failed to connect to %s", ep);
        } else {
            logInfo("[ix-net] ATSStore: connected to %s", ep);
        }
        return connected;
    }

    /// Create an attestation using GenerateAndCreateAttestation.
    /// Returns true on success, sets error string on failure.
    bool createAttestation(ref const AttestationCommand cmd, ref string error) {
        if (!connected) {
            error = "ATSStore client not connected to " ~ endpoint;
            logError("[ix-net] ATSStore: createAttestation failed — not connected to %s", endpoint);
            return false;
        }

        auto requestBytes = encodeGenerateAttestationRequest(authToken, cmd);
        auto responseBytes = client.call(
            "/protocol.ATSStoreService/GenerateAndCreateAttestation",
            requestBytes
        );

        if (responseBytes.length == 0) {
            error = "empty response from ATSStore at " ~ endpoint;
            logError("[ix-net] ATSStore: empty response from %s", endpoint);
            return false;
        }

        auto resp = decodeGenerateAttestationResponse(responseBytes);
        if (!resp.success) {
            error = resp.error;
            logError("[ix-net] ATSStore: attestation rejected: %s", resp.error);
            return false;
        }
        return true;
    }

    /// Query attestations using a filter.
    /// Returns attestations on success, empty array on failure.
    Attestation[] getAttestations(ref const AttestationFilter filter, ref string error) {
        if (!connected) {
            error = "ATSStore client not connected to " ~ endpoint;
            return [];
        }

        auto requestBytes = encodeGetAttestationsRequest(authToken, filter);
        auto responseBytes = client.call(
            "/protocol.ATSStoreService/GetAttestations",
            requestBytes
        );

        if (responseBytes.length == 0) {
            error = "empty response from ATSStore at " ~ endpoint;
            return [];
        }

        auto resp = decode!GetAttestationsResponse(responseBytes);
        if (!resp.success) {
            error = resp.error;
            logError("[ix-net] ATSStore: query failed: %s", resp.error);
            return [];
        }
        return resp.attestations;
    }

    void close() {
        client.close();
        connected = false;
    }
}
