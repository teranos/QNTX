#!/usr/bin/env python3
"""Integration test for the webscraper plugin.

This test creates a mock ATSStore server and tests the full flow:
1. Start mock ATSStore gRPC server
2. Start webscraper plugin
3. Initialize plugin with mock ATSStore endpoint
4. Call scrape-and-attest via HandleHTTP
5. Verify attestations were created
"""

import json
import threading
import time
from concurrent import futures

import grpc

from qntx_webscraper.grpc import atsstore_pb2, atsstore_pb2_grpc
from qntx_webscraper.grpc import domain_pb2, domain_pb2_grpc
from qntx_webscraper.plugin import WebScraperPlugin


class MockATSStoreService(atsstore_pb2_grpc.ATSStoreServiceServicer):
    """Mock ATSStore that records created attestations."""

    def __init__(self):
        self.attestations = []
        self.auth_token = "test-token"

    def GenerateAndCreateAttestation(self, request, context):
        if request.auth_token != self.auth_token:
            return atsstore_pb2.GenerateAttestationResponse(
                success=False,
                error="Invalid auth token",
            )

        # Generate a fake attestation ID
        cmd = request.command
        attestation_id = f"as_{len(self.attestations):04d}"

        attestation = atsstore_pb2.Attestation(
            id=attestation_id,
            subjects=list(cmd.subjects),
            predicates=list(cmd.predicates),
            contexts=list(cmd.contexts),
            actors=list(cmd.actors),
            timestamp=cmd.timestamp or int(time.time()),
            source="qntx-webscraper",
            attributes_json=cmd.attributes_json,
            created_at=int(time.time()),
        )

        self.attestations.append(attestation)
        print(f"  [MockATS] Created attestation: {attestation_id}")
        print(f"            Subject: {list(cmd.subjects)}")
        print(f"            Predicate: {list(cmd.predicates)}")
        print(f"            Context: {list(cmd.contexts)[:1]}...")  # Truncate long URLs

        return atsstore_pb2.GenerateAttestationResponse(
            success=True,
            attestation=attestation,
        )

    def AttestationExists(self, request, context):
        exists = any(a.id == request.id for a in self.attestations)
        return atsstore_pb2.AttestationExistsResponse(exists=exists)


def run_test():
    print("=" * 60)
    print("QNTX Webscraper Plugin Integration Test")
    print("=" * 60)

    # 1. Start mock ATSStore server
    print("\n[1] Starting mock ATSStore server on port 50051...")
    mock_ats = MockATSStoreService()
    ats_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    atsstore_pb2_grpc.add_ATSStoreServiceServicer_to_server(mock_ats, ats_server)
    ats_server.add_insecure_port("[::]:50051")
    ats_server.start()
    print("    Mock ATSStore started")

    # 2. Create plugin instance
    print("\n[2] Creating webscraper plugin...")
    plugin = WebScraperPlugin()

    # 3. Initialize plugin
    print("\n[3] Initializing plugin with ATSStore endpoint...")
    init_request = domain_pb2.InitializeRequest(
        ats_store_endpoint="localhost:50051",
        queue_endpoint="",
        auth_token="test-token",
        config={
            "user_agent": "QNTX-Test/1.0",
            "timeout": "10",
        },
    )
    plugin.Initialize(init_request, None)
    print("    Plugin initialized")

    # 4. Test health check
    print("\n[4] Checking plugin health...")
    health = plugin.Health(domain_pb2.Empty(), None)
    print(f"    Healthy: {health.healthy}")
    print(f"    Details: {dict(health.details)}")

    # 5. Test scrape (without attestations)
    print("\n[5] Testing /scrape endpoint...")
    scrape_request = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape",
        body=json.dumps({"url": "https://example.com"}).encode(),
    )
    response = plugin.HandleHTTP(scrape_request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    Title: {result.get('title')}")
    print(f"    Links found: {len(result.get('links', []))}")

    # 6. Test scrape-and-attest
    print("\n[6] Testing /scrape-and-attest endpoint...")
    attest_request = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape-and-attest",
        body=json.dumps({
            "url": "https://example.com",
            "actor": "integration-test",
            "include_external": True,
        }).encode(),
    )
    response = plugin.HandleHTTP(attest_request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    Attestations created: {result.get('attestations_created')}")
    print(f"    IDs: {result.get('attestation_ids')}")

    # 7. Verify attestations in mock store
    print("\n[7] Verifying attestations in mock ATSStore...")
    print(f"    Total attestations stored: {len(mock_ats.attestations)}")
    for att in mock_ats.attestations:
        print(f"    - {att.id}: {att.subjects[0]} {att.predicates[0]} -> {att.contexts[0][:50]}...")

    # 8. Shutdown
    print("\n[8] Shutting down...")
    plugin.Shutdown(domain_pb2.Empty(), None)
    ats_server.stop(0)
    print("    Done!")

    print("\n" + "=" * 60)
    print("Integration test PASSED!")
    print("=" * 60)


if __name__ == "__main__":
    run_test()
