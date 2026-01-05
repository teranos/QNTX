#!/usr/bin/env python3
"""Demo of QNTX WebScraper plugin features."""

import json
import grpc
from qntx_webscraper.grpc import domain_pb2, domain_pb2_grpc

def pretty_print_json(data):
    """Pretty print JSON data."""
    print(json.dumps(data, indent=2))

def demo_plugin():
    # Connect to the plugin
    print("=" * 60)
    print("QNTX WebScraper Plugin Demo")
    print("=" * 60)

    channel = grpc.insecure_channel('localhost:50052')
    stub = domain_pb2_grpc.DomainPluginServiceStub(channel)

    # 1. Get metadata
    print("\n1. PLUGIN METADATA")
    print("-" * 40)
    metadata = stub.Metadata(domain_pb2.Empty())
    print(f"Name: {metadata.name}")
    print(f"Version: {metadata.version}")
    print(f"Description: {metadata.description}")

    # 2. Initialize
    print("\n2. INITIALIZING PLUGIN")
    print("-" * 40)
    init_req = domain_pb2.InitializeRequest(
        ats_store_endpoint="",  # No real ATSStore for demo
        queue_endpoint="",
        auth_token="demo-token",
        config={
            "user_agent": "QNTX-Demo/1.0",
            "timeout": "15",
            "respect_robots": "false",  # Disable for demo
            "rate_limit": "5.0",
            "allow_private_ips": "true",
            "max_response_size": str(5 * 1024 * 1024),  # 5MB
        }
    )
    stub.Initialize(init_req)
    print("✓ Plugin initialized with demo configuration")

    # 3. Basic scraping
    print("\n3. BASIC WEB SCRAPING")
    print("-" * 40)
    print("Scraping example.com...")

    http_req = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape",
        headers=[
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"])
        ],
        body=json.dumps({"url": "https://example.com"}).encode()
    )

    response = stub.HandleHTTP(http_req)
    if response.status_code == 200:
        data = json.loads(response.body)
        print(f"✓ Title: {data.get('title', '')}")
        print(f"✓ Links found: {len(data.get('links', []))}")
        if data.get('links'):
            print("  Sample links:")
            for link in data['links'][:3]:
                print(f"    - {link.get('target_url', '')}")
    else:
        print(f"✗ Error: {response.body.decode()}")

    # 4. Extended scraping with metadata
    print("\n4. EXTENDED SCRAPING (with metadata)")
    print("-" * 40)
    print("Scraping with full content extraction...")

    http_req = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape-full",
        headers=[
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"])
        ],
        body=json.dumps({"url": "https://www.python.org"}).encode()
    )

    response = stub.HandleHTTP(http_req)
    if response.status_code == 200:
        data = json.loads(response.body)
        print(f"✓ Title: {data.get('title', '')[:50]}...")
        print(f"✓ Links: {len(data.get('links', []))}")

        meta = data.get('meta', {})
        if meta.get('description'):
            print(f"✓ Description: {meta['description'][:80]}...")
        if meta.get('og_title'):
            print(f"✓ Open Graph Title: {meta['og_title']}")

        images = data.get('images', [])
        if images:
            print(f"✓ Images found: {len(images)}")

        headings = data.get('headings', {})
        if headings:
            print(f"✓ Headings structure:")
            for level, texts in headings.items():
                print(f"    {level}: {len(texts)} heading(s)")
    else:
        print(f"✗ Error: {response.body.decode()}")

    # 5. RSS Feed parsing
    print("\n5. RSS FEED PARSING")
    print("-" * 40)
    print("Parsing RSS feed from BBC News...")

    http_req = domain_pb2.HTTPRequest(
        method="POST",
        path="/feed",
        headers=[
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"])
        ],
        body=json.dumps({"url": "http://feeds.bbci.co.uk/news/technology/rss.xml"}).encode()
    )

    response = stub.HandleHTTP(http_req)
    if response.status_code == 200:
        data = json.loads(response.body)
        print(f"✓ Feed Title: {data.get('title', '')}")
        print(f"✓ Feed Type: {data.get('feed_type', '')}")
        items = data.get('items', [])
        print(f"✓ Items: {len(items)}")
        if items:
            print("  Latest items:")
            for item in items[:3]:
                print(f"    - {item.get('title', '')[:60]}...")
                if item.get('published'):
                    print(f"      Published: {item['published']}")
    else:
        print(f"✗ Error: {response.body.decode()}")

    # 6. Sitemap parsing
    print("\n6. SITEMAP PARSING")
    print("-" * 40)
    print("Parsing sitemap from Python.org...")

    http_req = domain_pb2.HTTPRequest(
        method="POST",
        path="/sitemap",
        headers=[
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"])
        ],
        body=json.dumps({"url": "https://www.python.org/sitemap.xml"}).encode()
    )

    response = stub.HandleHTTP(http_req)
    if response.status_code == 200:
        data = json.loads(response.body)
        urls = data.get('urls', [])
        nested = data.get('sitemaps', [])
        print(f"✓ URLs found: {len(urls)}")
        print(f"✓ Nested sitemaps: {len(nested)}")
        if urls:
            print("  Sample URLs:")
            for url in urls[:3]:
                print(f"    - {url.get('loc', '')}")
                if url.get('lastmod'):
                    print(f"      Last modified: {url['lastmod']}")
    else:
        print(f"✗ Error: {response.body.decode()}")

    # 7. Security features demo
    print("\n7. SECURITY FEATURES")
    print("-" * 40)

    # Try to scrape localhost (should be blocked by default)
    print("Testing SSRF protection (trying to scrape localhost)...")

    # First reinitialize with security enabled
    init_req = domain_pb2.InitializeRequest(
        ats_store_endpoint="",
        queue_endpoint="",
        auth_token="demo-token",
        config={
            "user_agent": "QNTX-Demo/1.0",
            "timeout": "15",
            "respect_robots": "false",
            "rate_limit": "5.0",
            "allow_private_ips": "false",  # Disable for security test
            "max_response_size": str(5 * 1024 * 1024),
        }
    )
    stub.Initialize(init_req)

    http_req = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape",
        headers=[
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"])
        ],
        body=json.dumps({"url": "http://localhost:8080"}).encode()
    )

    response = stub.HandleHTTP(http_req)
    if response.status_code == 200:
        data = json.loads(response.body)
        if data.get('error'):
            print(f"✓ SSRF Protection working: {data['error']}")
        else:
            print("✗ Warning: localhost was not blocked!")
    else:
        print(f"✓ Request blocked: {response.body.decode()}")

    # Test cloud metadata endpoint blocking
    print("\nTesting cloud metadata endpoint blocking...")
    http_req = domain_pb2.HTTPRequest(
        method="POST",
        path="/scrape",
        headers=[
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"])
        ],
        body=json.dumps({"url": "http://169.254.169.254/latest/meta-data"}).encode()
    )

    response = stub.HandleHTTP(http_req)
    if response.status_code == 200:
        data = json.loads(response.body)
        if data.get('error'):
            print(f"✓ Cloud metadata blocking working: {data['error']}")
        else:
            print("✗ Warning: cloud metadata endpoint was not blocked!")

    print("\n" + "=" * 60)
    print("Demo completed successfully!")
    print("=" * 60)

    # Shutdown
    stub.Shutdown(domain_pb2.Empty())
    channel.close()

if __name__ == "__main__":
    try:
        demo_plugin()
    except grpc.RpcError as e:
        print(f"\n❌ gRPC Error: {e}")
        print("\nMake sure the plugin server is running:")
        print("  python run_plugin.py --port 50052")
    except Exception as e:
        print(f"\n❌ Error: {e}")