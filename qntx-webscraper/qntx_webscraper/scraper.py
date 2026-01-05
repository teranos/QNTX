"""Web scraper for extracting URLs and creating attestations."""

from __future__ import annotations

import ipaddress
import json
import logging
import socket
import time
from dataclasses import dataclass, field
from typing import Iterator
from urllib.parse import urljoin, urlparse
from urllib.robotparser import RobotFileParser

import requests
from bs4 import BeautifulSoup
from lxml import etree

from .atsstore import ATSStoreClient, AttestationCommand, AttestationFilter


class SSRFError(ValueError):
    """Raised when a URL is blocked due to SSRF protection."""

    pass


logger = logging.getLogger(__name__)


@dataclass
class ExtractedLink:
    """A link extracted from a web page."""

    source_url: str  # The page the link was found on
    target_url: str  # The URL the link points to
    anchor_text: str  # The text content of the link
    rel: list[str] = field(default_factory=list)  # rel attribute values
    is_external: bool = False  # True if link points to different domain


@dataclass
class ExtractedImage:
    """An image extracted from a web page."""

    src: str
    alt: str
    title: str = ""
    width: int | None = None
    height: int | None = None


@dataclass
class ExtractedMeta:
    """Metadata extracted from a web page."""

    description: str = ""
    keywords: list[str] = field(default_factory=list)
    author: str = ""
    published_date: str = ""
    modified_date: str = ""
    # Open Graph
    og_title: str = ""
    og_description: str = ""
    og_image: str = ""
    og_type: str = ""
    og_url: str = ""
    # Twitter Card
    twitter_card: str = ""
    twitter_title: str = ""
    twitter_description: str = ""
    twitter_image: str = ""
    # Canonical
    canonical_url: str = ""
    # Language
    language: str = ""


@dataclass
class StructuredData:
    """JSON-LD or microdata extracted from a page."""

    type: str  # e.g., "Article", "Product", "Organization"
    data: dict  # The full structured data object


@dataclass
class ScrapeResult:
    """Result of scraping a web page."""

    url: str
    title: str
    links: list[ExtractedLink]
    status_code: int
    error: str | None = None
    # Extended content
    meta: ExtractedMeta = field(default_factory=ExtractedMeta)
    images: list[ExtractedImage] = field(default_factory=list)
    structured_data: list[StructuredData] = field(default_factory=list)
    headings: dict[str, list[str]] = field(default_factory=dict)  # h1, h2, etc.


@dataclass
class FeedItem:
    """An item from an RSS/Atom feed."""

    title: str
    link: str
    description: str = ""
    published: str = ""
    author: str = ""
    guid: str = ""
    categories: list[str] = field(default_factory=list)


@dataclass
class FeedResult:
    """Result of parsing a feed."""

    url: str
    title: str
    description: str
    items: list[FeedItem]
    feed_type: str  # "rss" or "atom"
    error: str | None = None


@dataclass
class SitemapURL:
    """A URL from a sitemap."""

    loc: str
    lastmod: str = ""
    changefreq: str = ""
    priority: float = 0.5


@dataclass
class SitemapResult:
    """Result of parsing a sitemap."""

    url: str
    urls: list[SitemapURL]
    sitemaps: list[str]  # Nested sitemap URLs (from sitemap index)
    error: str | None = None


class RateLimiter:
    """Simple rate limiter for polite crawling."""

    def __init__(self, requests_per_second: float = 1.0):
        self.min_interval = 1.0 / requests_per_second
        self.last_request: dict[str, float] = {}  # domain -> timestamp

    def wait(self, url: str) -> None:
        """Wait if needed to respect rate limit for this domain."""
        domain = urlparse(url).netloc
        now = time.time()

        if domain in self.last_request:
            elapsed = now - self.last_request[domain]
            if elapsed < self.min_interval:
                sleep_time = self.min_interval - elapsed
                logger.debug(f"Rate limiting: sleeping {sleep_time:.2f}s for {domain}")
                time.sleep(sleep_time)

        self.last_request[domain] = time.time()


class RobotsChecker:
    """Checks robots.txt before scraping."""

    def __init__(self, user_agent: str, timeout: int = 10):
        self.user_agent = user_agent
        self.timeout = timeout
        self.parsers: dict[str, RobotFileParser] = {}
        self.session = requests.Session()

    def can_fetch(self, url: str) -> bool:
        """Check if we're allowed to fetch this URL."""
        parsed = urlparse(url)
        robots_url = f"{parsed.scheme}://{parsed.netloc}/robots.txt"

        if robots_url not in self.parsers:
            parser = RobotFileParser()
            try:
                response = self.session.get(robots_url, timeout=self.timeout)
                if response.status_code == 200:
                    parser.parse(response.text.splitlines())
                else:
                    # No robots.txt or error - assume allowed
                    parser.parse([])
            except requests.RequestException:
                # Can't fetch robots.txt - assume allowed
                parser.parse([])
            self.parsers[robots_url] = parser

        return self.parsers[robots_url].can_fetch(self.user_agent, url)

    def get_crawl_delay(self, url: str) -> float | None:
        """Get the crawl-delay for this domain, if specified."""
        parsed = urlparse(url)
        robots_url = f"{parsed.scheme}://{parsed.netloc}/robots.txt"

        if robots_url in self.parsers:
            delay = self.parsers[robots_url].crawl_delay(self.user_agent)
            return delay
        return None


class WebScraper:
    """Scrapes web pages and extracts URLs for attestation."""

    # Standard predicates for web attestations
    PREDICATE_LINKS_TO = "links_to"
    PREDICATE_HAS_TITLE = "has_title"
    PREDICATE_EXTERNAL_LINK = "links_externally_to"
    # Extended predicates
    PREDICATE_HAS_DESCRIPTION = "has_meta_description"
    PREDICATE_HAS_IMAGE = "has_image"
    PREDICATE_AUTHORED_BY = "authored_by"
    PREDICATE_PUBLISHED_AT = "published_at"
    PREDICATE_HAS_CANONICAL = "has_canonical_url"
    PREDICATE_HAS_STRUCTURED_DATA = "has_structured_data"
    PREDICATE_FEED_CONTAINS = "feed_contains"
    PREDICATE_SITEMAP_CONTAINS = "sitemap_contains"

    # Cloud metadata endpoints to block (SSRF protection)
    BLOCKED_METADATA_HOSTS = {
        "169.254.169.254",  # AWS/GCP/Azure metadata
        "metadata.google.internal",
        "metadata.goog",
    }

    def __init__(
        self,
        ats_client: ATSStoreClient | None = None,
        user_agent: str = "QNTX-WebScraper/0.1",
        timeout: int = 30,
        respect_robots: bool = True,
        rate_limit: float = 1.0,  # requests per second
        max_response_size: int = 10 * 1024 * 1024,  # 10MB default
        allow_private_ips: bool = False,  # SSRF protection
    ):
        """Initialize the scraper.

        Args:
            ats_client: Optional ATSStore client for persisting attestations
            user_agent: User agent string for requests
            timeout: Request timeout in seconds
            respect_robots: Whether to check robots.txt before scraping
            rate_limit: Maximum requests per second per domain
            max_response_size: Maximum response size in bytes (default 10MB)
            allow_private_ips: Allow scraping private/internal IPs (default False for SSRF protection)
        """
        self.ats_client = ats_client
        self.user_agent = user_agent
        self.timeout = timeout
        self.max_response_size = max_response_size
        self.allow_private_ips = allow_private_ips
        self.session = requests.Session()
        self.session.headers.update({"User-Agent": user_agent})

        # Robots.txt and rate limiting
        self.respect_robots = respect_robots
        self.robots_checker = (
            RobotsChecker(user_agent, timeout) if respect_robots else None
        )
        self.rate_limiter = RateLimiter(rate_limit) if rate_limit > 0 else None

    def _check_robots(self, url: str) -> bool:
        """Check if we can fetch this URL according to robots.txt."""
        if not self.robots_checker:
            return True
        allowed = self.robots_checker.can_fetch(url)
        if not allowed:
            logger.info(f"Blocked by robots.txt: {url}")
        return allowed

    def _rate_limit(self, url: str) -> None:
        """Apply rate limiting for this URL's domain."""
        if self.rate_limiter:
            self.rate_limiter.wait(url)

    def _validate_url(self, url: str) -> None:
        """Validate URL is safe to fetch (SSRF protection).

        Args:
            url: The URL to validate

        Raises:
            SSRFError: If the URL is blocked for security reasons
        """
        parsed = urlparse(url)

        # Only allow http/https
        if parsed.scheme not in ("http", "https"):
            raise SSRFError(f"Unsupported scheme: {parsed.scheme}")

        hostname = parsed.hostname
        if not hostname:
            raise SSRFError("URL has no hostname")

        # Block localhost/loopback (unless allow_private_ips is set)
        if not self.allow_private_ips:
            if hostname.lower() in ("localhost", "127.0.0.1", "::1", "0.0.0.0"):
                raise SSRFError(f"Cannot scrape localhost: {hostname}")

        # Block cloud metadata endpoints (always blocked - security critical)
        if hostname.lower() in self.BLOCKED_METADATA_HOSTS:
            raise SSRFError(f"Cannot scrape cloud metadata endpoint: {hostname}")

        # Check if hostname is an IP address
        try:
            ip = ipaddress.ip_address(hostname)
            if not self.allow_private_ips:
                if ip.is_private:
                    raise SSRFError(f"Cannot scrape private IP: {hostname}")
                if ip.is_loopback:
                    raise SSRFError(f"Cannot scrape loopback IP: {hostname}")
                if ip.is_link_local:
                    raise SSRFError(f"Cannot scrape link-local IP: {hostname}")
                if ip.is_reserved:
                    raise SSRFError(f"Cannot scrape reserved IP: {hostname}")
        except ValueError:
            # Not an IP address, resolve hostname and check
            if not self.allow_private_ips:
                try:
                    resolved = socket.gethostbyname(hostname)
                    ip = ipaddress.ip_address(resolved)
                    if ip.is_private or ip.is_loopback or ip.is_link_local:
                        raise SSRFError(
                            f"Hostname {hostname} resolves to blocked IP: {resolved}"
                        )
                except socket.gaierror:
                    pass  # Can't resolve, let request handle it

    def _fetch_with_size_limit(
        self, url: str, expected_content_types: list[str] | None = None
    ) -> bytes:
        """Fetch URL with size limit and optional content-type validation.

        Args:
            url: The URL to fetch
            expected_content_types: List of expected content-type substrings (e.g., ["text/html", "application/xhtml"])

        Returns:
            Response content as bytes

        Raises:
            ValueError: If response is too large or wrong content type
            requests.RequestException: If request fails
        """
        response = self.session.get(url, timeout=self.timeout, stream=True)
        response.raise_for_status()

        # Validate content type if specified
        if expected_content_types:
            content_type = response.headers.get("Content-Type", "").lower()
            if not any(ct in content_type for ct in expected_content_types):
                logger.warning(f"Unexpected Content-Type for {url}: {content_type}")

        # Check content length header
        content_length = response.headers.get("Content-Length")
        if content_length and int(content_length) > self.max_response_size:
            raise ValueError(
                f"Response too large: {content_length} bytes (max: {self.max_response_size})"
            )

        # Read with size limit
        content = b""
        for chunk in response.iter_content(chunk_size=8192):
            content += chunk
            if len(content) > self.max_response_size:
                raise ValueError(f"Response exceeded {self.max_response_size} bytes")

        return content

    def _extract_meta(self, soup: BeautifulSoup, url: str) -> ExtractedMeta:
        """Extract metadata from the page."""
        meta = ExtractedMeta()

        # Basic meta tags
        desc_tag = soup.find("meta", attrs={"name": "description"})
        if desc_tag and desc_tag.get("content"):
            meta.description = desc_tag["content"]

        keywords_tag = soup.find("meta", attrs={"name": "keywords"})
        if keywords_tag and keywords_tag.get("content"):
            meta.keywords = [k.strip() for k in keywords_tag["content"].split(",")]

        author_tag = soup.find("meta", attrs={"name": "author"})
        if author_tag and author_tag.get("content"):
            meta.author = author_tag["content"]

        # Published/modified dates
        for date_prop in ["article:published_time", "datePublished", "date"]:
            date_tag = soup.find("meta", attrs={"property": date_prop}) or soup.find(
                "meta", attrs={"name": date_prop}
            )
            if date_tag and date_tag.get("content"):
                meta.published_date = date_tag["content"]
                break

        for date_prop in ["article:modified_time", "dateModified"]:
            date_tag = soup.find("meta", attrs={"property": date_prop})
            if date_tag and date_tag.get("content"):
                meta.modified_date = date_tag["content"]
                break

        # Open Graph
        og_props = {
            "og:title": "og_title",
            "og:description": "og_description",
            "og:image": "og_image",
            "og:type": "og_type",
            "og:url": "og_url",
        }
        for prop, attr in og_props.items():
            tag = soup.find("meta", attrs={"property": prop})
            if tag and tag.get("content"):
                setattr(meta, attr, tag["content"])

        # Twitter Card
        twitter_props = {
            "twitter:card": "twitter_card",
            "twitter:title": "twitter_title",
            "twitter:description": "twitter_description",
            "twitter:image": "twitter_image",
        }
        for prop, attr in twitter_props.items():
            tag = soup.find("meta", attrs={"name": prop})
            if tag and tag.get("content"):
                setattr(meta, attr, tag["content"])

        # Canonical URL
        canonical = soup.find("link", attrs={"rel": "canonical"})
        if canonical and canonical.get("href"):
            meta.canonical_url = urljoin(url, canonical["href"])

        # Language
        html_tag = soup.find("html")
        if html_tag and html_tag.get("lang"):
            meta.language = html_tag["lang"]

        return meta

    def _extract_images(self, soup: BeautifulSoup, url: str) -> list[ExtractedImage]:
        """Extract images from the page."""
        images = []
        for img in soup.find_all("img", src=True):
            src = urljoin(url, img["src"])
            images.append(
                ExtractedImage(
                    src=src,
                    alt=img.get("alt", ""),
                    title=img.get("title", ""),
                    width=int(img["width"]) if img.get("width", "").isdigit() else None,
                    height=(
                        int(img["height"]) if img.get("height", "").isdigit() else None
                    ),
                )
            )
        return images

    def _extract_structured_data(self, soup: BeautifulSoup) -> list[StructuredData]:
        """Extract JSON-LD structured data from the page."""
        structured = []
        for script in soup.find_all("script", type="application/ld+json"):
            try:
                data = json.loads(script.string)
                # Handle @graph arrays
                if isinstance(data, dict) and "@graph" in data:
                    for item in data["@graph"]:
                        if isinstance(item, dict) and "@type" in item:
                            structured.append(
                                StructuredData(type=item["@type"], data=item)
                            )
                elif isinstance(data, dict) and "@type" in data:
                    structured.append(StructuredData(type=data["@type"], data=data))
                elif isinstance(data, list):
                    for item in data:
                        if isinstance(item, dict) and "@type" in item:
                            structured.append(
                                StructuredData(type=item["@type"], data=item)
                            )
            except (json.JSONDecodeError, TypeError):
                continue
        return structured

    def _extract_headings(self, soup: BeautifulSoup) -> dict[str, list[str]]:
        """Extract headings from the page."""
        headings = {}
        for level in range(1, 7):
            tag = f"h{level}"
            found = [h.get_text(strip=True) for h in soup.find_all(tag)]
            if found:
                headings[tag] = found
        return headings

    def scrape(self, url: str, extract_all: bool = False) -> ScrapeResult:
        """Scrape a URL and extract content.

        Args:
            url: The URL to scrape
            extract_all: If True, extract meta, images, structured data, headings

        Returns:
            ScrapeResult with extracted content
        """
        # SSRF protection
        try:
            self._validate_url(url)
        except SSRFError as e:
            logger.warning(f"SSRF blocked: {e}")
            return ScrapeResult(
                url=url,
                title="",
                links=[],
                status_code=0,
                error=str(e),
            )

        # Check robots.txt
        if not self._check_robots(url):
            return ScrapeResult(
                url=url,
                title="",
                links=[],
                status_code=0,
                error="Blocked by robots.txt",
            )

        # Apply rate limiting
        self._rate_limit(url)

        try:
            content = self._fetch_with_size_limit(
                url, expected_content_types=["text/html", "application/xhtml"]
            )
        except (requests.RequestException, ValueError) as e:
            logger.error(f"Failed to fetch {url}: {e}")
            return ScrapeResult(
                url=url,
                title="",
                links=[],
                status_code=0,
                error=str(e),
            )

        soup = BeautifulSoup(content, "lxml")
        title = soup.title.string if soup.title else ""
        source_domain = urlparse(url).netloc

        # Extract links
        links = []
        for anchor in soup.find_all("a", href=True):
            href = anchor["href"]
            absolute_url = urljoin(url, href)

            parsed = urlparse(absolute_url)
            if parsed.scheme not in ("http", "https"):
                continue

            target_domain = parsed.netloc
            is_external = target_domain != source_domain

            rel = anchor.get("rel", [])
            if isinstance(rel, str):
                rel = rel.split()

            links.append(
                ExtractedLink(
                    source_url=url,
                    target_url=absolute_url,
                    anchor_text=anchor.get_text(strip=True),
                    rel=rel,
                    is_external=is_external,
                )
            )

        result = ScrapeResult(
            url=url,
            title=title or "",
            links=links,
            status_code=200,  # Success if we got here
        )

        # Extended extraction
        if extract_all:
            result.meta = self._extract_meta(soup, url)
            result.images = self._extract_images(soup, url)
            result.structured_data = self._extract_structured_data(soup)
            result.headings = self._extract_headings(soup)

        logger.info(f"Scraped {url}: {len(links)} links")
        return result

    def scrape_and_attest(
        self,
        url: str,
        actor: str = "",
        include_external: bool = True,
        extract_all: bool = True,
    ) -> tuple[ScrapeResult, list[str]]:
        """Scrape a URL and create attestations for found content.

        Args:
            url: The URL to scrape
            actor: The actor creating the attestations
            include_external: Whether to include external links
            extract_all: Extract and attest meta, images, structured data

        Returns:
            Tuple of (ScrapeResult, list of created attestation IDs)
        """
        if not self.ats_client:
            raise RuntimeError("No ATSStore client configured")

        result = self.scrape(url, extract_all=extract_all)
        if result.error:
            return result, []

        attestation_ids = []

        # Title
        if result.title:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_HAS_TITLE],
                contexts=[result.title],
                actors=[actor] if actor else [],
                attributes={"source": "qntx-webscraper"},
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        # Meta description
        if result.meta.description:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_HAS_DESCRIPTION],
                contexts=[result.meta.description],
                actors=[actor] if actor else [],
                attributes={"source": "qntx-webscraper"},
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        # Author
        if result.meta.author:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_AUTHORED_BY],
                contexts=[result.meta.author],
                actors=[actor] if actor else [],
                attributes={"source": "qntx-webscraper"},
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        # Published date
        if result.meta.published_date:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_PUBLISHED_AT],
                contexts=[result.meta.published_date],
                actors=[actor] if actor else [],
                attributes={"source": "qntx-webscraper"},
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        # Canonical URL
        if result.meta.canonical_url and result.meta.canonical_url != url:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_HAS_CANONICAL],
                contexts=[result.meta.canonical_url],
                actors=[actor] if actor else [],
                attributes={"source": "qntx-webscraper"},
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        # Images (limit to first 10)
        for img in result.images[:10]:
            if img.alt:  # Only attest images with alt text
                cmd = AttestationCommand(
                    subjects=[url],
                    predicates=[self.PREDICATE_HAS_IMAGE],
                    contexts=[img.src],
                    actors=[actor] if actor else [],
                    attributes={
                        "source": "qntx-webscraper",
                        "alt": img.alt,
                        "title": img.title,
                    },
                )
                att = self.ats_client.generate_and_create(cmd)
                attestation_ids.append(att.id)

        # Structured data
        for sd in result.structured_data:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_HAS_STRUCTURED_DATA],
                contexts=[sd.type],
                actors=[actor] if actor else [],
                attributes={
                    "source": "qntx-webscraper",
                    "data": json.dumps(sd.data),
                },
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        # Links
        for link in result.links:
            if not include_external and link.is_external:
                continue

            predicate = (
                self.PREDICATE_EXTERNAL_LINK
                if link.is_external
                else self.PREDICATE_LINKS_TO
            )
            attributes = {"source": "qntx-webscraper"}
            if link.anchor_text:
                attributes["anchor_text"] = link.anchor_text
            if link.rel:
                attributes["rel"] = ",".join(link.rel)

            cmd = AttestationCommand(
                subjects=[url],
                predicates=[predicate],
                contexts=[link.target_url],
                actors=[actor] if actor else [],
                attributes=attributes,
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        logger.info(f"Created {len(attestation_ids)} attestations for {url}")
        return result, attestation_ids

    # ==================== RSS/Atom Feed Support ====================

    def scrape_feed(self, url: str) -> FeedResult:
        """Parse an RSS or Atom feed.

        Args:
            url: The feed URL

        Returns:
            FeedResult with parsed items
        """
        # SSRF protection
        try:
            self._validate_url(url)
        except SSRFError as e:
            logger.warning(f"SSRF blocked: {e}")
            return FeedResult(
                url=url,
                title="",
                description="",
                items=[],
                feed_type="unknown",
                error=str(e),
            )

        self._rate_limit(url)

        try:
            content = self._fetch_with_size_limit(
                url,
                expected_content_types=[
                    "application/rss",
                    "application/atom",
                    "application/xml",
                    "text/xml",
                ],
            )
        except (requests.RequestException, ValueError) as e:
            return FeedResult(
                url=url,
                title="",
                description="",
                items=[],
                feed_type="unknown",
                error=str(e),
            )

        try:
            root = etree.fromstring(content)
        except etree.XMLSyntaxError as e:
            return FeedResult(
                url=url,
                title="",
                description="",
                items=[],
                feed_type="unknown",
                error=f"Invalid XML: {e}",
            )

        # Detect feed type
        if root.tag == "rss" or root.find("channel") is not None:
            return self._parse_rss(url, root)
        elif root.tag.endswith("feed") or "{http://www.w3.org/2005/Atom}" in root.tag:
            return self._parse_atom(url, root)
        else:
            return FeedResult(
                url=url,
                title="",
                description="",
                items=[],
                feed_type="unknown",
                error="Unknown feed format",
            )

    def _parse_rss(self, url: str, root: etree._Element) -> FeedResult:
        """Parse RSS 2.0 feed."""
        channel = root.find("channel")
        if channel is None:
            channel = root  # Some RSS feeds have items at root

        title = channel.findtext("title", "")
        description = channel.findtext("description", "")

        items = []
        for item in channel.findall("item"):
            categories = [cat.text for cat in item.findall("category") if cat.text]
            items.append(
                FeedItem(
                    title=item.findtext("title", ""),
                    link=item.findtext("link", ""),
                    description=item.findtext("description", ""),
                    published=item.findtext("pubDate", ""),
                    author=item.findtext("author", "")
                    or item.findtext("{http://purl.org/dc/elements/1.1/}creator", ""),
                    guid=item.findtext("guid", ""),
                    categories=categories,
                )
            )

        return FeedResult(
            url=url,
            title=title,
            description=description,
            items=items,
            feed_type="rss",
        )

    def _parse_atom(self, url: str, root: etree._Element) -> FeedResult:
        """Parse Atom feed."""
        ns = {"atom": "http://www.w3.org/2005/Atom"}

        title_el = root.find("atom:title", ns) or root.find("title")
        title = title_el.text if title_el is not None else ""

        subtitle_el = root.find("atom:subtitle", ns) or root.find("subtitle")
        description = subtitle_el.text if subtitle_el is not None else ""

        items = []
        for entry in root.findall("atom:entry", ns) or root.findall("entry"):
            # Get link
            link = ""
            for link_el in entry.findall("atom:link", ns) or entry.findall("link"):
                href = link_el.get("href", "")
                rel = link_el.get("rel", "alternate")
                if rel == "alternate" and href:
                    link = href
                    break
                elif not link and href:
                    link = href

            title_el = entry.find("atom:title", ns) or entry.find("title")
            summary_el = entry.find("atom:summary", ns) or entry.find("summary")
            content_el = entry.find("atom:content", ns) or entry.find("content")
            published_el = entry.find("atom:published", ns) or entry.find("published")
            updated_el = entry.find("atom:updated", ns) or entry.find("updated")
            author_el = entry.find("atom:author/atom:name", ns) or entry.find(
                "author/name"
            )
            id_el = entry.find("atom:id", ns) or entry.find("id")

            categories = []
            for cat in entry.findall("atom:category", ns) or entry.findall("category"):
                term = cat.get("term", "")
                if term:
                    categories.append(term)

            items.append(
                FeedItem(
                    title=title_el.text if title_el is not None else "",
                    link=link,
                    description=(summary_el.text if summary_el is not None else "")
                    or (content_el.text if content_el is not None else ""),
                    published=(published_el.text if published_el is not None else "")
                    or (updated_el.text if updated_el is not None else ""),
                    author=author_el.text if author_el is not None else "",
                    guid=id_el.text if id_el is not None else "",
                    categories=categories,
                )
            )

        return FeedResult(
            url=url,
            title=title,
            description=description,
            items=items,
            feed_type="atom",
        )

    def scrape_feed_and_attest(
        self,
        url: str,
        actor: str = "",
    ) -> tuple[FeedResult, list[str]]:
        """Parse a feed and create attestations for each item.

        Args:
            url: The feed URL
            actor: The actor creating attestations

        Returns:
            Tuple of (FeedResult, list of attestation IDs)
        """
        if not self.ats_client:
            raise RuntimeError("No ATSStore client configured")

        result = self.scrape_feed(url)
        if result.error:
            return result, []

        attestation_ids = []

        # Attest feed title
        if result.title:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_HAS_TITLE],
                contexts=[result.title],
                actors=[actor] if actor else [],
                attributes={"source": "qntx-webscraper", "feed_type": result.feed_type},
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        # Attest each feed item
        for item in result.items:
            if not item.link:
                continue

            attributes = {"source": "qntx-webscraper"}
            if item.title:
                attributes["title"] = item.title
            if item.published:
                attributes["published"] = item.published
            if item.author:
                attributes["author"] = item.author

            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_FEED_CONTAINS],
                contexts=[item.link],
                actors=[actor] if actor else [],
                attributes=attributes,
            )
            att = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(att.id)

        logger.info(f"Created {len(attestation_ids)} attestations for feed {url}")
        return result, attestation_ids

    # ==================== Sitemap Support ====================

    def scrape_sitemap(self, url: str) -> SitemapResult:
        """Parse a sitemap.xml file.

        Args:
            url: The sitemap URL

        Returns:
            SitemapResult with parsed URLs
        """
        # SSRF protection
        try:
            self._validate_url(url)
        except SSRFError as e:
            logger.warning(f"SSRF blocked: {e}")
            return SitemapResult(url=url, urls=[], sitemaps=[], error=str(e))

        self._rate_limit(url)

        try:
            content = self._fetch_with_size_limit(
                url, expected_content_types=["application/xml", "text/xml"]
            )
        except (requests.RequestException, ValueError) as e:
            return SitemapResult(url=url, urls=[], sitemaps=[], error=str(e))

        try:
            root = etree.fromstring(content)
        except etree.XMLSyntaxError as e:
            return SitemapResult(
                url=url, urls=[], sitemaps=[], error=f"Invalid XML: {e}"
            )

        # Handle namespace
        ns = {"sm": "http://www.sitemaps.org/schemas/sitemap/0.9"}

        urls = []
        sitemaps = []

        # Check if it's a sitemap index
        for sitemap in root.findall("sm:sitemap", ns) or root.findall("sitemap"):
            loc = sitemap.findtext("sm:loc", "", ns) or sitemap.findtext("loc", "")
            if loc:
                sitemaps.append(loc)

        # Parse regular URLs
        for url_el in root.findall("sm:url", ns) or root.findall("url"):
            loc = url_el.findtext("sm:loc", "", ns) or url_el.findtext("loc", "")
            if not loc:
                continue

            lastmod = url_el.findtext("sm:lastmod", "", ns) or url_el.findtext(
                "lastmod", ""
            )
            changefreq = url_el.findtext("sm:changefreq", "", ns) or url_el.findtext(
                "changefreq", ""
            )
            priority_str = url_el.findtext("sm:priority", "", ns) or url_el.findtext(
                "priority", ""
            )

            try:
                priority = float(priority_str) if priority_str else 0.5
            except ValueError:
                priority = 0.5

            urls.append(
                SitemapURL(
                    loc=loc,
                    lastmod=lastmod,
                    changefreq=changefreq,
                    priority=priority,
                )
            )

        return SitemapResult(url=url, urls=urls, sitemaps=sitemaps)

    def scrape_sitemap_and_attest(
        self,
        url: str,
        actor: str = "",
        follow_nested: bool = True,
        max_nested: int = 10,
    ) -> tuple[list[SitemapResult], list[str]]:
        """Parse sitemap(s) and create attestations for each URL.

        Args:
            url: The sitemap URL
            actor: The actor creating attestations
            follow_nested: Whether to follow nested sitemaps
            max_nested: Maximum nested sitemaps to follow

        Returns:
            Tuple of (list of SitemapResults, list of attestation IDs)
        """
        if not self.ats_client:
            raise RuntimeError("No ATSStore client configured")

        results = []
        attestation_ids = []
        to_process = [url]
        processed = set()

        while to_process and len(processed) < max_nested:
            current_url = to_process.pop(0)
            if current_url in processed:
                continue
            processed.add(current_url)

            result = self.scrape_sitemap(current_url)
            results.append(result)

            if result.error:
                continue

            # Queue nested sitemaps (avoid duplicates in queue)
            if follow_nested:
                for nested in result.sitemaps:
                    if nested not in processed and nested not in to_process:
                        to_process.append(nested)

            # Create attestations for URLs
            for sitemap_url in result.urls:
                attributes = {"source": "qntx-webscraper"}
                if sitemap_url.lastmod:
                    attributes["lastmod"] = sitemap_url.lastmod
                if sitemap_url.changefreq:
                    attributes["changefreq"] = sitemap_url.changefreq
                attributes["priority"] = str(sitemap_url.priority)

                cmd = AttestationCommand(
                    subjects=[current_url],
                    predicates=[self.PREDICATE_SITEMAP_CONTAINS],
                    contexts=[sitemap_url.loc],
                    actors=[actor] if actor else [],
                    attributes=attributes,
                )
                att = self.ats_client.generate_and_create(cmd)
                attestation_ids.append(att.id)

        logger.info(
            f"Created {len(attestation_ids)} attestations from {len(results)} sitemaps"
        )
        return results, attestation_ids

    # ==================== Crawling ====================

    def _was_previously_crawled(self, url: str) -> bool:
        """Check if a URL was previously crawled by querying ATSStore.

        Args:
            url: The URL to check

        Returns:
            True if attestations exist for this URL (indicating it was crawled before)
        """
        if not self.ats_client:
            return False

        try:
            # Query for any attestation where this URL is the subject
            # with our webscraper predicates
            filter = AttestationFilter(
                subjects=[url],
                predicates=[
                    self.PREDICATE_HAS_TITLE
                ],  # If we have a title, we crawled it
                limit=1,  # We only need to know if any exist
            )
            results = self.ats_client.get_attestations(filter)
            return len(results) > 0
        except Exception as e:
            logger.warning(f"Failed to check ATSStore for {url}: {e}")
            return False  # Assume not crawled if check fails

    def _get_previously_crawled_urls(self, urls: list[str]) -> set[str]:
        """Batch check which URLs were previously crawled.

        Args:
            urls: List of URLs to check

        Returns:
            Set of URLs that have been previously crawled
        """
        if not self.ats_client or not urls:
            return set()

        previously_crawled = set()
        try:
            # Query for attestations where any of these URLs are subjects
            # Note: This is a batch query for efficiency
            for url in urls:
                if self._was_previously_crawled(url):
                    previously_crawled.add(url)
        except Exception as e:
            logger.warning(f"Failed to batch check ATSStore: {e}")

        return previously_crawled

    def crawl(
        self,
        start_url: str,
        max_pages: int = 10,
        same_domain_only: bool = True,
        skip_previously_crawled: bool = False,
    ) -> Iterator[ScrapeResult]:
        """Crawl starting from a URL, following links.

        Args:
            start_url: The starting URL
            max_pages: Maximum number of pages to crawl
            same_domain_only: Only follow links on the same domain
            skip_previously_crawled: Skip URLs that have attestations in ATSStore

        Yields:
            ScrapeResult for each page crawled
        """
        visited: set[str] = set()
        to_visit: list[str] = [start_url]

        # Check if start URL was previously crawled
        if skip_previously_crawled and self._was_previously_crawled(start_url):
            logger.info(f"Skipping previously crawled URL: {start_url}")
            return

        while to_visit and len(visited) < max_pages:
            url = to_visit.pop(0)
            if url in visited:
                continue

            # Check ATSStore for previously crawled URLs
            if skip_previously_crawled and self._was_previously_crawled(url):
                logger.info(f"Skipping previously crawled URL: {url}")
                visited.add(url)  # Mark as visited to avoid re-checking
                continue

            visited.add(url)
            result = self.scrape(url)
            yield result

            if result.error:
                continue

            # Queue new URLs to visit
            for link in result.links:
                if link.target_url in visited or link.target_url in to_visit:
                    continue
                if same_domain_only and link.is_external:
                    continue
                to_visit.append(link.target_url)

    def crawl_and_attest(
        self,
        start_url: str,
        actor: str = "",
        max_pages: int = 10,
        same_domain_only: bool = True,
        skip_previously_crawled: bool = True,
    ) -> tuple[list[ScrapeResult], list[str]]:
        """Crawl and create attestations for all found links.

        Args:
            start_url: The starting URL
            actor: The actor creating the attestations
            max_pages: Maximum number of pages to crawl
            same_domain_only: Only follow links on the same domain
            skip_previously_crawled: Skip URLs that have attestations in ATSStore (default True)

        Returns:
            Tuple of (list of ScrapeResults, list of all attestation IDs)
        """
        if not self.ats_client:
            raise RuntimeError("No ATSStore client configured")

        all_results = []
        all_attestation_ids = []

        for result in self.crawl(
            start_url,
            max_pages,
            same_domain_only,
            skip_previously_crawled=skip_previously_crawled,
        ):
            all_results.append(result)

            if result.error:
                continue

            # Create attestations for this page
            _, attestation_ids = self.scrape_and_attest(
                result.url,
                actor=actor,
                include_external=not same_domain_only,
            )
            all_attestation_ids.extend(attestation_ids)

        return all_results, all_attestation_ids
