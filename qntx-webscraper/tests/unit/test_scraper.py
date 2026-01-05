"""Unit tests for the WebScraper class."""

import pytest
import responses
from qntx_webscraper.scraper import WebScraper, SSRFError


class TestSSRFProtection:
    """Test SSRF protection mechanisms."""

    def test_blocks_localhost(self):
        """Test that localhost is blocked when allow_private_ips=False."""
        scraper = WebScraper(allow_private_ips=False)

        with pytest.raises(SSRFError, match="localhost"):
            scraper._validate_url("http://localhost/test")

        with pytest.raises(SSRFError, match="127.0.0.1"):
            scraper._validate_url("http://127.0.0.1/test")

    def test_blocks_private_ips(self):
        """Test that private IPs are blocked."""
        scraper = WebScraper(allow_private_ips=False)

        with pytest.raises(SSRFError, match="private IP"):
            scraper._validate_url("http://192.168.1.1/test")

        with pytest.raises(SSRFError, match="private IP"):
            scraper._validate_url("http://10.0.0.1/test")

    def test_blocks_cloud_metadata(self):
        """Test that cloud metadata endpoints are always blocked."""
        scraper = WebScraper(allow_private_ips=True)  # Even with this enabled

        with pytest.raises(SSRFError, match="cloud metadata"):
            scraper._validate_url("http://169.254.169.254/latest/meta-data")

        with pytest.raises(SSRFError, match="cloud metadata"):
            scraper._validate_url("http://metadata.google.internal/")

    def test_allows_public_urls(self):
        """Test that public URLs are allowed."""
        scraper = WebScraper()

        # Should not raise
        scraper._validate_url("https://example.com/test")
        scraper._validate_url("http://github.com/test")


class TestResponseSizeLimits:
    """Test response size limiting."""

    @responses.activate
    def test_respects_size_limit(self):
        """Test that responses exceeding size limit are rejected."""
        scraper = WebScraper(max_response_size=100)  # 100 bytes limit

        # Mock a large response
        responses.add(
            responses.GET,
            "https://example.com/large",
            body="x" * 200,  # 200 bytes
            status=200,
            headers={"Content-Type": "text/html"}
        )

        with pytest.raises(ValueError, match="exceeded.*bytes"):
            scraper._fetch_with_size_limit("https://example.com/large")

    @responses.activate
    def test_content_type_validation(self):
        """Test that content type is validated when specified."""
        scraper = WebScraper()

        responses.add(
            responses.GET,
            "https://example.com/json",
            json={"test": "data"},
            status=200,
            headers={"Content-Type": "application/json"}
        )

        # Should log warning but not fail
        content = scraper._fetch_with_size_limit(
            "https://example.com/json",
            expected_content_types=["text/html"]
        )
        assert content  # Should still return content


class TestWebScraping:
    """Test core web scraping functionality."""

    @responses.activate
    def test_scrape_basic(self):
        """Test basic HTML scraping."""
        html = """
        <html>
            <head><title>Test Page</title></head>
            <body>
                <a href="/page1">Link 1</a>
                <a href="https://external.com/page">External</a>
            </body>
        </html>
        """

        responses.add(
            responses.GET,
            "https://example.com/test",
            body=html,
            status=200,
            headers={"Content-Type": "text/html"}
        )

        scraper = WebScraper(respect_robots=False)
        result = scraper.scrape("https://example.com/test")

        assert result.title == "Test Page"
        assert len(result.links) == 2
        assert result.links[0].target_url == "https://example.com/page1"
        assert result.links[0].is_external is False
        assert result.links[1].is_external is True

    @responses.activate
    def test_scrape_with_metadata(self):
        """Test scraping with extended metadata extraction."""
        html = """
        <html lang="en">
            <head>
                <title>Test Page</title>
                <meta name="description" content="Test description">
                <meta property="og:title" content="OG Title">
                <meta property="og:image" content="https://example.com/image.jpg">
            </head>
            <body>
                <h1>Main Heading</h1>
                <img src="/test.jpg" alt="Test image">
            </body>
        </html>
        """

        responses.add(
            responses.GET,
            "https://example.com/test",
            body=html,
            status=200,
            headers={"Content-Type": "text/html"}
        )

        scraper = WebScraper(respect_robots=False)
        result = scraper.scrape("https://example.com/test", extract_all=True)

        assert result.meta.description == "Test description"
        assert result.meta.og_title == "OG Title"
        assert result.meta.og_image == "https://example.com/image.jpg"
        assert result.meta.language == "en"
        assert len(result.images) == 1
        assert result.images[0].alt == "Test image"
        assert "h1" in result.headings
        assert result.headings["h1"][0] == "Main Heading"


class TestFeedParsing:
    """Test RSS/Atom feed parsing."""

    @responses.activate
    def test_parse_rss_feed(self):
        """Test RSS 2.0 feed parsing."""
        rss = """<?xml version="1.0"?>
        <rss version="2.0">
            <channel>
                <title>Test Feed</title>
                <description>Test Description</description>
                <item>
                    <title>Item 1</title>
                    <link>https://example.com/item1</link>
                    <description>Description 1</description>
                    <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
                </item>
            </channel>
        </rss>
        """

        responses.add(
            responses.GET,
            "https://example.com/feed.rss",
            body=rss,
            status=200,
            headers={"Content-Type": "application/rss+xml"}
        )

        scraper = WebScraper()
        result = scraper.scrape_feed("https://example.com/feed.rss")

        assert result.feed_type == "rss"
        assert result.title == "Test Feed"
        assert len(result.items) == 1
        assert result.items[0].title == "Item 1"
        assert result.items[0].link == "https://example.com/item1"