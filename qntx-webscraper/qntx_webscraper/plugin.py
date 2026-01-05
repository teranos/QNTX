"""QNTX Domain Plugin implementation for web scraping."""

from __future__ import annotations

import json
import logging
from concurrent import futures
from dataclasses import asdict

import grpc

from .atsstore import ATSStoreClient
from .grpc import domain_pb2, domain_pb2_grpc
from .pulse import PulseClient
from .scraper import WebScraper

logger = logging.getLogger(__name__)


class WebScraperPlugin(domain_pb2_grpc.DomainPluginServiceServicer):
    """gRPC service implementing the QNTX DomainPlugin interface."""

    def __init__(self):
        self.ats_client: ATSStoreClient | None = None
        self.pulse_client: PulseClient | None = None
        self.scraper: WebScraper | None = None
        self.config: dict[str, str] = {}

    def Metadata(self, request, context):
        """Return plugin metadata."""
        return domain_pb2.MetadataResponse(
            name="webscraper",
            version="0.2.0",
            qntx_version=">=0.1.0",
            description="Web scraping plugin with feed/sitemap support, robots.txt, and rate limiting",
            author="QNTX",
            license="MIT",
        )

    def Initialize(self, request, context):
        """Initialize the plugin with service endpoints."""
        logger.info("Initializing webscraper plugin")
        logger.info(f"ATSStore endpoint: {request.ats_store_endpoint}")
        logger.info(f"Queue endpoint: {request.queue_endpoint}")

        self.config = dict(request.config)

        # Connect to ATSStore
        if request.ats_store_endpoint:
            self.ats_client = ATSStoreClient(
                endpoint=request.ats_store_endpoint,
                auth_token=request.auth_token,
            )

        # Connect to Pulse queue (optional)
        if request.queue_endpoint:
            self.pulse_client = PulseClient(
                endpoint=request.queue_endpoint,
                auth_token=request.auth_token,
            )
            logger.info("Pulse queue connected")

        # Create scraper with ATSStore client
        self.scraper = WebScraper(
            ats_client=self.ats_client,
            user_agent=self.config.get("user_agent", "QNTX-WebScraper/0.2"),
            timeout=int(self.config.get("timeout", "30")),
            respect_robots=self.config.get("respect_robots", "true").lower() == "true",
            rate_limit=float(self.config.get("rate_limit", "1.0")),
            max_response_size=int(self.config.get("max_response_size", str(10 * 1024 * 1024))),
            allow_private_ips=self.config.get("allow_private_ips", "false").lower() == "true",
        )

        logger.info("Webscraper plugin initialized successfully")
        return domain_pb2.Empty()

    def Shutdown(self, request, context):
        """Shutdown the plugin."""
        logger.info("Shutting down webscraper plugin")
        if self.ats_client:
            self.ats_client.close()
            self.ats_client = None
        if self.pulse_client:
            self.pulse_client.close()
            self.pulse_client = None
        self.scraper = None
        return domain_pb2.Empty()

    def HandleHTTP(self, request, context):
        """Handle HTTP requests to the plugin.

        Endpoints:
            POST /scrape - Scrape a URL (basic extraction)
            POST /scrape-full - Scrape with full metadata extraction
            POST /scrape-and-attest - Scrape and create attestations
            POST /feed - Parse RSS/Atom feed
            POST /feed-and-attest - Parse feed and create attestations
            POST /sitemap - Parse sitemap.xml
            POST /sitemap-and-attest - Parse sitemap and create attestations
            POST /crawl - Crawl from a URL and create attestations
            POST /schedule/scrape - Schedule a scrape job via Pulse
            POST /schedule/feed - Schedule a feed scrape job
            POST /schedule/sitemap - Schedule a sitemap scrape job
            POST /schedule/crawl - Schedule a crawl job
            GET /jobs - List scheduled jobs
        """
        path = request.path
        method = request.method

        logger.debug(f"HTTP request: {method} {path}")

        try:
            body = {}
            if request.body:
                body = json.loads(request.body.decode("utf-8"))

            # GET endpoints
            if method == "GET":
                if path == "/jobs":
                    return self._handle_list_jobs(body)
                return self._error_response(404, f"Unknown GET endpoint: {path}")

            # POST endpoints
            if method != "POST":
                return self._error_response(405, "Method not allowed")

            # Basic scraping
            if path == "/scrape":
                return self._handle_scrape(body, extract_all=False)
            elif path == "/scrape-full":
                return self._handle_scrape(body, extract_all=True)
            elif path == "/scrape-and-attest":
                return self._handle_scrape_and_attest(body)

            # Feed parsing
            elif path == "/feed":
                return self._handle_feed(body)
            elif path == "/feed-and-attest":
                return self._handle_feed_and_attest(body)

            # Sitemap parsing
            elif path == "/sitemap":
                return self._handle_sitemap(body)
            elif path == "/sitemap-and-attest":
                return self._handle_sitemap_and_attest(body)

            # Crawling
            elif path == "/crawl":
                return self._handle_crawl(body)

            # Pulse scheduling
            elif path == "/schedule/scrape":
                return self._handle_schedule_scrape(body)
            elif path == "/schedule/feed":
                return self._handle_schedule_feed(body)
            elif path == "/schedule/sitemap":
                return self._handle_schedule_sitemap(body)
            elif path == "/schedule/crawl":
                return self._handle_schedule_crawl(body)

            else:
                return self._error_response(404, f"Unknown endpoint: {path}")

        except json.JSONDecodeError as e:
            return self._error_response(400, f"Invalid JSON: {e}")
        except Exception as e:
            logger.exception(f"Error handling request: {e}")
            return self._error_response(500, str(e))

    # ==================== Scraping Handlers ====================

    def _handle_scrape(self, body: dict, extract_all: bool) -> domain_pb2.HTTPResponse:
        """Handle /scrape and /scrape-full endpoints."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        result = self.scraper.scrape(url, extract_all=extract_all)

        response_data = {
            "url": result.url,
            "title": result.title,
            "status_code": result.status_code,
            "error": result.error,
            "links": [
                {
                    "target_url": link.target_url,
                    "anchor_text": link.anchor_text,
                    "is_external": link.is_external,
                    "rel": link.rel,
                }
                for link in result.links
            ],
        }

        # Add extended data if extracted
        if extract_all:
            response_data["meta"] = {
                "description": result.meta.description,
                "keywords": result.meta.keywords,
                "author": result.meta.author,
                "published_date": result.meta.published_date,
                "canonical_url": result.meta.canonical_url,
                "language": result.meta.language,
                "og_title": result.meta.og_title,
                "og_description": result.meta.og_description,
                "og_image": result.meta.og_image,
            }
            response_data["images"] = [
                {"src": img.src, "alt": img.alt, "title": img.title}
                for img in result.images[:20]  # Limit to 20
            ]
            response_data["structured_data"] = [
                {"type": sd.type, "data": sd.data}
                for sd in result.structured_data
            ]
            response_data["headings"] = result.headings

        return self._json_response(200, response_data)

    def _handle_scrape_and_attest(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /scrape-and-attest endpoint."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        actor = body.get("actor", "")
        include_external = body.get("include_external", True)
        extract_all = body.get("extract_all", True)

        result, attestation_ids = self.scraper.scrape_and_attest(
            url,
            actor=actor,
            include_external=include_external,
            extract_all=extract_all,
        )

        response_data = {
            "url": result.url,
            "title": result.title,
            "status_code": result.status_code,
            "error": result.error,
            "links_count": len(result.links),
            "images_count": len(result.images),
            "structured_data_count": len(result.structured_data),
            "attestations_created": len(attestation_ids),
            "attestation_ids": attestation_ids,
        }

        return self._json_response(200, response_data)

    # ==================== Feed Handlers ====================

    def _handle_feed(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /feed endpoint."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        result = self.scraper.scrape_feed(url)

        response_data = {
            "url": result.url,
            "title": result.title,
            "description": result.description,
            "feed_type": result.feed_type,
            "error": result.error,
            "items": [
                {
                    "title": item.title,
                    "link": item.link,
                    "description": item.description[:200] if item.description else "",
                    "published": item.published,
                    "author": item.author,
                    "categories": item.categories,
                }
                for item in result.items
            ],
        }

        return self._json_response(200, response_data)

    def _handle_feed_and_attest(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /feed-and-attest endpoint."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        actor = body.get("actor", "")

        result, attestation_ids = self.scraper.scrape_feed_and_attest(url, actor=actor)

        response_data = {
            "url": result.url,
            "title": result.title,
            "feed_type": result.feed_type,
            "error": result.error,
            "items_count": len(result.items),
            "attestations_created": len(attestation_ids),
            "attestation_ids": attestation_ids,
        }

        return self._json_response(200, response_data)

    # ==================== Sitemap Handlers ====================

    def _handle_sitemap(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /sitemap endpoint."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        result = self.scraper.scrape_sitemap(url)

        response_data = {
            "url": result.url,
            "error": result.error,
            "urls_count": len(result.urls),
            "nested_sitemaps": result.sitemaps,
            "urls": [
                {
                    "loc": u.loc,
                    "lastmod": u.lastmod,
                    "changefreq": u.changefreq,
                    "priority": u.priority,
                }
                for u in result.urls[:100]  # Limit to 100
            ],
        }

        return self._json_response(200, response_data)

    def _handle_sitemap_and_attest(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /sitemap-and-attest endpoint."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        actor = body.get("actor", "")
        follow_nested = body.get("follow_nested", True)
        max_nested = body.get("max_nested", 10)

        results, attestation_ids = self.scraper.scrape_sitemap_and_attest(
            url,
            actor=actor,
            follow_nested=follow_nested,
            max_nested=max_nested,
        )

        response_data = {
            "start_url": url,
            "sitemaps_processed": len(results),
            "total_urls": sum(len(r.urls) for r in results),
            "attestations_created": len(attestation_ids),
            "sitemaps": [
                {
                    "url": r.url,
                    "urls_count": len(r.urls),
                    "nested_count": len(r.sitemaps),
                    "error": r.error,
                }
                for r in results
            ],
        }

        return self._json_response(200, response_data)

    # ==================== Crawl Handler ====================

    def _handle_crawl(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /crawl endpoint."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        actor = body.get("actor", "")
        max_pages = body.get("max_pages", 10)
        same_domain_only = body.get("same_domain_only", True)

        results, attestation_ids = self.scraper.crawl_and_attest(
            url,
            actor=actor,
            max_pages=max_pages,
            same_domain_only=same_domain_only,
        )

        response_data = {
            "start_url": url,
            "pages_crawled": len(results),
            "total_links": sum(len(r.links) for r in results),
            "attestations_created": len(attestation_ids),
            "pages": [
                {
                    "url": r.url,
                    "title": r.title,
                    "links_count": len(r.links),
                    "error": r.error,
                }
                for r in results
            ],
        }

        return self._json_response(200, response_data)

    # ==================== Pulse Scheduling Handlers ====================

    def _handle_schedule_scrape(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /schedule/scrape endpoint."""
        if not self.pulse_client:
            return self._error_response(503, "Pulse queue not configured")

        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        job_id = self.pulse_client.schedule_scrape(
            url=url,
            actor=body.get("actor", ""),
            extract_all=body.get("extract_all", True),
        )

        return self._json_response(200, {"job_id": job_id, "status": "queued"})

    def _handle_schedule_feed(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /schedule/feed endpoint."""
        if not self.pulse_client:
            return self._error_response(503, "Pulse queue not configured")

        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        job_id = self.pulse_client.schedule_feed_scrape(
            url=url,
            actor=body.get("actor", ""),
        )

        return self._json_response(200, {"job_id": job_id, "status": "queued"})

    def _handle_schedule_sitemap(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /schedule/sitemap endpoint."""
        if not self.pulse_client:
            return self._error_response(503, "Pulse queue not configured")

        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        job_id = self.pulse_client.schedule_sitemap_scrape(
            url=url,
            actor=body.get("actor", ""),
            follow_nested=body.get("follow_nested", True),
        )

        return self._json_response(200, {"job_id": job_id, "status": "queued"})

    def _handle_schedule_crawl(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /schedule/crawl endpoint."""
        if not self.pulse_client:
            return self._error_response(503, "Pulse queue not configured")

        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        job_id = self.pulse_client.schedule_crawl(
            url=url,
            actor=body.get("actor", ""),
            max_pages=body.get("max_pages", 10),
            same_domain_only=body.get("same_domain_only", True),
        )

        return self._json_response(200, {"job_id": job_id, "status": "queued"})

    def _handle_list_jobs(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /jobs endpoint."""
        if not self.pulse_client:
            return self._error_response(503, "Pulse queue not configured")

        status = body.get("status", "")
        limit = body.get("limit", 100)

        jobs = self.pulse_client.list_jobs(status=status, limit=limit)

        return self._json_response(200, {
            "jobs": [
                {
                    "id": j.id,
                    "handler": j.handler_name,
                    "status": j.status,
                    "progress": {"current": j.progress.current, "total": j.progress.total},
                    "error": j.error,
                    "created_at": j.created_at,
                }
                for j in jobs
            ]
        })

    # ==================== WebSocket & Health ====================

    def HandleWebSocket(self, request_iterator, context):
        """WebSocket handler (not implemented for this plugin)."""
        yield domain_pb2.WebSocketMessage(
            type=domain_pb2.WebSocketMessage.CLOSE,
            data=b"WebSocket not supported by webscraper plugin",
        )

    def Health(self, request, context):
        """Check plugin health."""
        healthy = self.scraper is not None
        details = {}

        if self.ats_client:
            details["ats_store"] = "connected"
        else:
            details["ats_store"] = "not connected"

        if self.pulse_client:
            details["pulse_queue"] = "connected"
        else:
            details["pulse_queue"] = "not configured"

        if self.scraper:
            details["respect_robots"] = str(self.scraper.respect_robots)
            details["rate_limit"] = str(self.scraper.rate_limiter.min_interval if self.scraper.rate_limiter else 0)

        return domain_pb2.HealthResponse(
            healthy=healthy,
            message="OK" if healthy else "Not initialized",
            details=details,
        )

    # ==================== Helpers ====================

    def _json_response(self, status_code: int, data: dict) -> domain_pb2.HTTPResponse:
        """Create a JSON HTTP response."""
        body = json.dumps(data).encode("utf-8")
        headers = [
            domain_pb2.HTTPHeader(name="Content-Type", values=["application/json"]),
        ]
        return domain_pb2.HTTPResponse(
            status_code=status_code,
            headers=headers,
            body=body,
        )

    def _error_response(self, status_code: int, message: str) -> domain_pb2.HTTPResponse:
        """Create an error HTTP response."""
        return self._json_response(status_code, {"error": message})


def serve(port: int = 9001):
    """Start the gRPC server.

    Args:
        port: Port to listen on (default 9001)
    """
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    domain_pb2_grpc.add_DomainPluginServiceServicer_to_server(
        WebScraperPlugin(),
        server,
    )
    server.add_insecure_port(f"[::]:{port}")
    server.start()
    logger.info(f"Webscraper plugin server started on port {port}")
    return server
