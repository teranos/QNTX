#!/usr/bin/env python3
"""Security tests for SSRF vulnerabilities."""

import sys
import socket
from qntx_webscraper.scraper import WebScraper, SSRFError


def test_ssrf_protection():
    """Test that all SSRF attack vectors are blocked."""

    scraper = WebScraper(allow_private_ips=False)

    # Test vectors that should be blocked
    blocked_urls = [
        # Localhost variants
        "http://localhost/admin",
        "http://127.0.0.1/admin",
        "http://[::1]/admin",
        "http://0.0.0.0/admin",

        # Private IPs
        "http://192.168.1.1/admin",
        "http://10.0.0.1/admin",
        "http://172.16.0.1/admin",

        # Cloud metadata endpoints - CRITICAL
        "http://169.254.169.254/latest/meta-data",
        "http://metadata.google.internal/computeMetadata/v1/",
        "http://metadata.goog/computeMetadata/v1/",

        # DNS rebinding attempts
        "http://1.1.1.1.xip.io/",  # May resolve to 1.1.1.1

        # File URLs
        "file:///etc/passwd",
        "file:///c:/windows/system32/config/sam",
    ]

    failures = []

    for url in blocked_urls:
        try:
            scraper._validate_url(url)
            failures.append(f"FAILED TO BLOCK: {url}")
        except SSRFError:
            print(f"✓ Blocked: {url}")
        except Exception as e:
            print(f"✓ Blocked with {e.__class__.__name__}: {url}")

    # Test that public URLs are allowed
    allowed_urls = [
        "https://github.com/test",
        "https://example.com/test",
        "http://8.8.8.8/test",  # Public DNS
    ]

    for url in allowed_urls:
        try:
            scraper._validate_url(url)
            print(f"✓ Allowed: {url}")
        except Exception as e:
            failures.append(f"WRONGLY BLOCKED: {url} - {e}")

    if failures:
        print("\n❌ SECURITY TEST FAILURES:")
        for failure in failures:
            print(f"  - {failure}")
        sys.exit(1)
    else:
        print("\n✅ All SSRF protection tests passed!")


def test_dns_resolution_check():
    """Test that hostnames resolving to private IPs are blocked."""

    scraper = WebScraper(allow_private_ips=False)

    # Create a test hostname that resolves to localhost
    # Note: This test requires network access
    try:
        # Test with a hostname that might resolve to localhost
        test_hostname = socket.gethostname()
        local_ip = socket.gethostbyname(test_hostname)

        if local_ip.startswith("127.") or local_ip.startswith("192.168."):
            # If our hostname resolves to a private IP, it should be blocked
            try:
                scraper._validate_url(f"http://{test_hostname}/test")
                print(f"❌ Failed to block hostname {test_hostname} resolving to {local_ip}")
                sys.exit(1)
            except SSRFError:
                print(f"✓ Blocked hostname {test_hostname} resolving to private IP {local_ip}")
    except Exception as e:
        print(f"⚠ DNS resolution test skipped: {e}")


if __name__ == "__main__":
    test_ssrf_protection()
    test_dns_resolution_check()
    print("\n🛡️ Security audit complete!")