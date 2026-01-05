"""Web scraper for extracting URLs and creating attestations."""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Iterator
from urllib.parse import urljoin, urlparse

import requests
from bs4 import BeautifulSoup

from .atsstore import ATSStoreClient, AttestationCommand

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
class ScrapeResult:
    """Result of scraping a web page."""

    url: str
    title: str
    links: list[ExtractedLink]
    status_code: int
    error: str | None = None


class WebScraper:
    """Scrapes web pages and extracts URLs for attestation."""

    # Standard predicates for web attestations
    PREDICATE_LINKS_TO = "links_to"
    PREDICATE_HAS_TITLE = "has_title"
    PREDICATE_EXTERNAL_LINK = "links_externally_to"

    def __init__(
        self,
        ats_client: ATSStoreClient | None = None,
        user_agent: str = "QNTX-WebScraper/0.1",
        timeout: int = 30,
    ):
        """Initialize the scraper.

        Args:
            ats_client: Optional ATSStore client for persisting attestations
            user_agent: User agent string for requests
            timeout: Request timeout in seconds
        """
        self.ats_client = ats_client
        self.user_agent = user_agent
        self.timeout = timeout
        self.session = requests.Session()
        self.session.headers.update({"User-Agent": user_agent})

    def scrape(self, url: str) -> ScrapeResult:
        """Scrape a URL and extract all links.

        Args:
            url: The URL to scrape

        Returns:
            ScrapeResult with extracted links
        """
        try:
            response = self.session.get(url, timeout=self.timeout)
            response.raise_for_status()
        except requests.RequestException as e:
            logger.error(f"Failed to fetch {url}: {e}")
            return ScrapeResult(
                url=url,
                title="",
                links=[],
                status_code=getattr(e.response, "status_code", 0) if hasattr(e, "response") else 0,
                error=str(e),
            )

        soup = BeautifulSoup(response.content, "lxml")
        title = soup.title.string if soup.title else ""
        source_domain = urlparse(url).netloc

        links = []
        for anchor in soup.find_all("a", href=True):
            href = anchor["href"]
            # Resolve relative URLs
            absolute_url = urljoin(url, href)

            # Skip non-http URLs (mailto:, javascript:, etc.)
            parsed = urlparse(absolute_url)
            if parsed.scheme not in ("http", "https"):
                continue

            # Determine if external
            target_domain = parsed.netloc
            is_external = target_domain != source_domain

            # Get rel attribute
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

        logger.info(f"Scraped {url}: found {len(links)} links")
        return ScrapeResult(
            url=url,
            title=title or "",
            links=links,
            status_code=response.status_code,
        )

    def scrape_and_attest(
        self,
        url: str,
        actor: str = "",
        include_external: bool = True,
    ) -> tuple[ScrapeResult, list[str]]:
        """Scrape a URL and create attestations for found links.

        Args:
            url: The URL to scrape
            actor: The actor creating the attestations (plugin ID if empty)
            include_external: Whether to include external links

        Returns:
            Tuple of (ScrapeResult, list of created attestation IDs)

        Raises:
            RuntimeError: If no ATSStore client is configured
        """
        if not self.ats_client:
            raise RuntimeError("No ATSStore client configured")

        result = self.scrape(url)
        if result.error:
            return result, []

        attestation_ids = []

        # Create attestation for page title
        if result.title:
            cmd = AttestationCommand(
                subjects=[url],
                predicates=[self.PREDICATE_HAS_TITLE],
                contexts=[result.title],
                actors=[actor] if actor else [],
                attributes={"source": "qntx-webscraper"},
            )
            attestation = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(attestation.id)
            logger.debug(f"Created title attestation: {attestation.id}")

        # Create attestations for links
        for link in result.links:
            if not include_external and link.is_external:
                continue

            predicate = self.PREDICATE_EXTERNAL_LINK if link.is_external else self.PREDICATE_LINKS_TO

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
            attestation = self.ats_client.generate_and_create(cmd)
            attestation_ids.append(attestation.id)
            logger.debug(f"Created link attestation: {attestation.id}")

        logger.info(f"Created {len(attestation_ids)} attestations for {url}")
        return result, attestation_ids

    def crawl(
        self,
        start_url: str,
        max_pages: int = 10,
        same_domain_only: bool = True,
    ) -> Iterator[ScrapeResult]:
        """Crawl starting from a URL, following links.

        Args:
            start_url: The starting URL
            max_pages: Maximum number of pages to crawl
            same_domain_only: Only follow links on the same domain

        Yields:
            ScrapeResult for each page crawled
        """
        visited: set[str] = set()
        to_visit: list[str] = [start_url]
        start_domain = urlparse(start_url).netloc

        while to_visit and len(visited) < max_pages:
            url = to_visit.pop(0)
            if url in visited:
                continue

            visited.add(url)
            result = self.scrape(url)
            yield result

            if result.error:
                continue

            # Queue new URLs to visit
            for link in result.links:
                if link.target_url in visited:
                    continue
                if same_domain_only and link.is_external:
                    continue
                if link.target_url not in to_visit:
                    to_visit.append(link.target_url)

    def crawl_and_attest(
        self,
        start_url: str,
        actor: str = "",
        max_pages: int = 10,
        same_domain_only: bool = True,
    ) -> tuple[list[ScrapeResult], list[str]]:
        """Crawl and create attestations for all found links.

        Args:
            start_url: The starting URL
            actor: The actor creating the attestations
            max_pages: Maximum number of pages to crawl
            same_domain_only: Only follow links on the same domain

        Returns:
            Tuple of (list of ScrapeResults, list of all attestation IDs)
        """
        if not self.ats_client:
            raise RuntimeError("No ATSStore client configured")

        all_results = []
        all_attestation_ids = []

        for result in self.crawl(start_url, max_pages, same_domain_only):
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
