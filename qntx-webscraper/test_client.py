#!/usr/bin/env python3
"""Test client for the QNTX webscraper plugin."""

import json
import grpc
from qntx_webscraper.grpc import domain_pb2, domain_pb2_grpc


def test_plugin():
    # Connect to the plugin
    channel = grpc.insecure_channel("localhost:50052")
    stub = domain_pb2_grpc.DomainPluginServiceStub(channel)

    # 1. Get metadata
    print("Getting metadata...")
    metadata = stub.Metadata(domain_pb2.Empty())
    print(f"  Name: {metadata.name}")
    print(f"  Version: {metadata.version}")
    print(f"  Description: {metadata.description}")

    # 2. Initialize (without real ATSStore for now)
    print("\nInitializing plugin...")
    init_req = domain_pb2.InitializeRequest(
        ats_store_endpoint="",  # No real ATSStore
        queue_endpoint="",
        auth_token="test-token",
        config={
            "user_agent": "QNTX-Test/1.0",
            "timeout": "10",
            "respect_robots": "false",  # Disable for testing
            "rate_limit": "10.0",
            "allow_private_ips": "true",  # Allow for testing
        },
    )
    stub.Initialize(init_req)
    print("  Initialized successfully")

    # 3. Check health
    print("\nChecking health...")
    health = stub.Health(domain_pb2.Empty())
    print(f"  Healthy: {health.healthy}")

    # 4. Test scraping a simple website
    print("\nTesting scrape on example.com...")
    http_req = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape",
        headers=[
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"])
        ],
        body=json.dumps({"url": "https://example.com"}).encode(),
    )

    response = stub.HandleHTTP(http_req)
    print(f"  Status: {response.status_code}")
    if response.status_code == 200:
        data = json.loads(response.body)
        print(f"  Title: {data.get('title', '')}")
        print(f"  Links found: {len(data.get('links', []))}")
    else:
        print(f"  Error: {response.body.decode()}")

    print("\nTest completed!")
    channel.close()


if __name__ == "__main__":
    test_plugin()
