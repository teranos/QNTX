#!/usr/bin/env python3
"""Integration test for the webscraper plugin.

This test creates a mock ATSStore server and tests the full flow:
1. Start mock HTTP server for test pages
2. Start mock ATSStore gRPC server
3. Start webscraper plugin
4. Call various endpoints via HandleHTTP
5. Verify attestations were created
"""

import json
import threading
import time
from concurrent import futures
from http.server import HTTPServer, BaseHTTPRequestHandler

import grpc

from qntx_webscraper.grpc import atsstore_pb2, atsstore_pb2_grpc
from qntx_webscraper.grpc import domain_pb2
from qntx_webscraper.plugin import WebScraperPlugin


# ==================== Mock HTTP Server ====================

MOCK_HTML = """<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Test Page</title>
    <meta name="description" content="A test page for scraping">
    <meta name="author" content="Test Author">
    <meta property="og:title" content="OG Test Title">
    <meta property="og:description" content="OG Description">
    <link rel="canonical" href="http://localhost:8888/canonical">
    <script type="application/ld+json">
    {"@type": "Article", "headline": "Test Article", "author": "JSON-LD Author"}
    </script>
</head>
<body>
    <h1>Welcome to Test Page</h1>
    <h2>Section 1</h2>
    <p>Some content here.</p>
    <a href="/page2">Internal Link</a>
    <a href="https://external.com/path">External Link</a>
    <img src="/image1.png" alt="Test Image 1">
    <img src="/image2.png" alt="Test Image 2">
</body>
</html>
"""

MOCK_RSS = """<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
    <title>Test RSS Feed</title>
    <description>A test RSS feed</description>
    <item>
        <title>Article 1</title>
        <link>http://localhost:8888/article1</link>
        <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
    </item>
    <item>
        <title>Article 2</title>
        <link>http://localhost:8888/article2</link>
        <pubDate>Tue, 02 Jan 2024 00:00:00 GMT</pubDate>
    </item>
</channel>
</rss>
"""

MOCK_SITEMAP = """<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    <url>
        <loc>http://localhost:8888/page1</loc>
        <lastmod>2024-01-01</lastmod>
        <priority>1.0</priority>
    </url>
    <url>
        <loc>http://localhost:8888/page2</loc>
        <lastmod>2024-01-02</lastmod>
        <priority>0.8</priority>
    </url>
</urlset>
"""


class MockHTTPHandler(BaseHTTPRequestHandler):
    """Mock HTTP server for testing."""

    def log_message(self, format, *args):
        pass  # Suppress logging

    def do_GET(self):
        if self.path == "/" or self.path == "/test":
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(MOCK_HTML.encode())
        elif self.path == "/feed.xml":
            self.send_response(200)
            self.send_header("Content-Type", "application/rss+xml")
            self.end_headers()
            self.wfile.write(MOCK_RSS.encode())
        elif self.path == "/sitemap.xml":
            self.send_response(200)
            self.send_header("Content-Type", "application/xml")
            self.end_headers()
            self.wfile.write(MOCK_SITEMAP.encode())
        elif self.path == "/robots.txt":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"User-agent: *\nAllow: /\n")
        else:
            self.send_response(404)
            self.end_headers()


# ==================== Mock ATSStore ====================


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
        return atsstore_pb2.GenerateAttestationResponse(
            success=True,
            attestation=attestation,
        )

    def AttestationExists(self, request, context):
        exists = any(a.id == request.id for a in self.attestations)
        return atsstore_pb2.AttestationExistsResponse(exists=exists)


def run_test():
    print("=" * 60)
    print("QNTX Webscraper Plugin Integration Test (Extended)")
    print("=" * 60)

    # 1. Start mock HTTP server
    print("\n[1] Starting mock HTTP server on port 8888...")
    http_server = HTTPServer(("localhost", 8888), MockHTTPHandler)
    http_thread = threading.Thread(target=http_server.serve_forever)
    http_thread.daemon = True
    http_thread.start()
    print("    Mock HTTP server started")

    # 2. Start mock ATSStore server
    print("\n[2] Starting mock ATSStore server on port 50051...")
    mock_ats = MockATSStoreService()
    ats_server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    atsstore_pb2_grpc.add_ATSStoreServiceServicer_to_server(mock_ats, ats_server)
    ats_server.add_insecure_port("[::]:50051")
    ats_server.start()
    print("    Mock ATSStore started")

    # 3. Create and initialize plugin
    print("\n[3] Creating and initializing webscraper plugin...")
    plugin = WebScraperPlugin()
    init_request = domain_pb2.InitializeRequest(
        ats_store_endpoint="localhost:50051",
        queue_endpoint="",
        auth_token="test-token",
        config={
            "user_agent": "QNTX-Test/1.0",
            "timeout": "10",
            "respect_robots": "true",
            "rate_limit": "10.0",  # Fast for testing
            "allow_private_ips": "true",  # Allow localhost for testing
        },
    )
    plugin.Initialize(init_request, None)
    print("    Plugin initialized")

    # 4. Check health
    print("\n[4] Checking plugin health...")
    health = plugin.Health(domain_pb2.Empty(), None)
    print(f"    Healthy: {health.healthy}")
    print(f"    Details: {dict(health.details)}")

    # 5. Test /scrape-full (extended extraction)
    print("\n[5] Testing /scrape-full endpoint...")
    request = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape-full",
        body=json.dumps({"url": "http://localhost:8888/test"}).encode(),
    )
    response = plugin.HandleHTTP(request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    Title: {result.get('title')}")
    print(f"    Links: {len(result.get('links', []))}")
    print(
        f"    Meta description: {result.get('meta', {}).get('description', '')[:50]}..."
    )
    print(f"    Meta author: {result.get('meta', {}).get('author')}")
    print(f"    Images: {len(result.get('images', []))}")
    print(f"    Structured data: {len(result.get('structured_data', []))}")
    print(f"    Headings: {result.get('headings', {})}")

    # 6. Test /scrape-and-attest
    print("\n[6] Testing /scrape-and-attest endpoint...")
    request = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape-and-attest",
        body=json.dumps(
            {
                "url": "http://localhost:8888/test",
                "actor": "integration-test",
                "extract_all": True,
            }
        ).encode(),
    )
    response = plugin.HandleHTTP(request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    Attestations created: {result.get('attestations_created')}")
    print(f"    Links count: {result.get('links_count')}")
    print(f"    Images count: {result.get('images_count')}")

    # 7. Test /feed endpoint
    print("\n[7] Testing /feed endpoint...")
    request = domain_pb2.HTTPRequest(
        method="POST",
        path="/feed",
        body=json.dumps({"url": "http://localhost:8888/feed.xml"}).encode(),
    )
    response = plugin.HandleHTTP(request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    Feed type: {result.get('feed_type')}")
    print(f"    Feed title: {result.get('title')}")
    print(f"    Items: {len(result.get('items', []))}")

    # 8. Test /feed-and-attest
    print("\n[8] Testing /feed-and-attest endpoint...")
    request = domain_pb2.HTTPRequest(
        method="POST",
        path="/feed-and-attest",
        body=json.dumps(
            {
                "url": "http://localhost:8888/feed.xml",
                "actor": "feed-test",
            }
        ).encode(),
    )
    response = plugin.HandleHTTP(request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    Attestations created: {result.get('attestations_created')}")

    # 9. Test /sitemap endpoint
    print("\n[9] Testing /sitemap endpoint...")
    request = domain_pb2.HTTPRequest(
        method="POST",
        path="/sitemap",
        body=json.dumps({"url": "http://localhost:8888/sitemap.xml"}).encode(),
    )
    response = plugin.HandleHTTP(request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    URLs found: {result.get('urls_count')}")

    # 10. Test /sitemap-and-attest
    print("\n[10] Testing /sitemap-and-attest endpoint...")
    request = domain_pb2.HTTPRequest(
        method="POST",
        path="/sitemap-and-attest",
        body=json.dumps(
            {
                "url": "http://localhost:8888/sitemap.xml",
                "actor": "sitemap-test",
            }
        ).encode(),
    )
    response = plugin.HandleHTTP(request, None)
    result = json.loads(response.body.decode())
    print(f"    Status: {response.status_code}")
    print(f"    Attestations created: {result.get('attestations_created')}")

    # 11. Verify attestations in mock store
    print("\n[11] Verifying attestations in mock ATSStore...")
    print(f"    Total attestations stored: {len(mock_ats.attestations)}")

    # Group by predicate
    predicates = {}
    for att in mock_ats.attestations:
        pred = att.predicates[0] if att.predicates else "unknown"
        predicates[pred] = predicates.get(pred, 0) + 1
    print("    By predicate:")
    for pred, count in sorted(predicates.items()):
        print(f"      - {pred}: {count}")

    # 12. Shutdown
    print("\n[12] Shutting down...")
    plugin.Shutdown(domain_pb2.Empty(), None)
    ats_server.stop(0)
    http_server.shutdown()
    print("    Done!")

    print("\n" + "=" * 60)
    print("Integration test PASSED!")
    print(f"Total attestations: {len(mock_ats.attestations)}")
    print("=" * 60)


if __name__ == "__main__":
    run_test()
