"""QNTX Domain Plugin implementation for web scraping."""

from __future__ import annotations

import json
import logging
from concurrent import futures

import grpc

from .atsstore import ATSStoreClient
from .grpc import domain_pb2, domain_pb2_grpc
from .scraper import WebScraper

logger = logging.getLogger(__name__)


class WebScraperPlugin(domain_pb2_grpc.DomainPluginServiceServicer):
    """gRPC service implementing the QNTX DomainPlugin interface."""

    def __init__(self):
        self.ats_client: ATSStoreClient | None = None
        self.scraper: WebScraper | None = None
        self.config: dict[str, str] = {}

    def Metadata(self, request, context):
        """Return plugin metadata."""
        return domain_pb2.MetadataResponse(
            name="webscraper",
            version="0.1.0",
            qntx_version=">=0.1.0",
            description="Web scraping plugin for extracting URLs and creating attestations",
            author="QNTX",
            license="MIT",
        )

    def Initialize(self, request, context):
        """Initialize the plugin with service endpoints."""
        logger.info(f"Initializing webscraper plugin")
        logger.info(f"ATSStore endpoint: {request.ats_store_endpoint}")

        self.config = dict(request.config)

        # Connect to ATSStore
        self.ats_client = ATSStoreClient(
            endpoint=request.ats_store_endpoint,
            auth_token=request.auth_token,
        )

        # Create scraper with ATSStore client
        self.scraper = WebScraper(
            ats_client=self.ats_client,
            user_agent=self.config.get("user_agent", "QNTX-WebScraper/0.1"),
            timeout=int(self.config.get("timeout", "30")),
        )

        logger.info("Webscraper plugin initialized successfully")
        return domain_pb2.Empty()

    def Shutdown(self, request, context):
        """Shutdown the plugin."""
        logger.info("Shutting down webscraper plugin")
        if self.ats_client:
            self.ats_client.close()
            self.ats_client = None
        self.scraper = None
        return domain_pb2.Empty()

    def HandleHTTP(self, request, context):
        """Handle HTTP requests to the plugin.

        Endpoints:
            POST /scrape - Scrape a URL and return extracted links
            POST /scrape-and-attest - Scrape and create attestations
            POST /crawl - Crawl from a URL and create attestations
        """
        path = request.path
        method = request.method

        logger.debug(f"HTTP request: {method} {path}")

        try:
            if method != "POST":
                return self._error_response(405, "Method not allowed")

            body = json.loads(request.body.decode("utf-8")) if request.body else {}

            if path == "/scrape":
                return self._handle_scrape(body)
            elif path == "/scrape-and-attest":
                return self._handle_scrape_and_attest(body)
            elif path == "/crawl":
                return self._handle_crawl(body)
            else:
                return self._error_response(404, f"Unknown endpoint: {path}")

        except json.JSONDecodeError as e:
            return self._error_response(400, f"Invalid JSON: {e}")
        except Exception as e:
            logger.exception(f"Error handling request: {e}")
            return self._error_response(500, str(e))

    def _handle_scrape(self, body: dict) -> domain_pb2.HTTPResponse:
        """Handle /scrape endpoint."""
        url = body.get("url")
        if not url:
            return self._error_response(400, "Missing 'url' field")

        if not self.scraper:
            return self._error_response(503, "Plugin not initialized")

        result = self.scraper.scrape(url)

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

        result, attestation_ids = self.scraper.scrape_and_attest(
            url,
            actor=actor,
            include_external=include_external,
        )

        response_data = {
            "url": result.url,
            "title": result.title,
            "status_code": result.status_code,
            "error": result.error,
            "links_count": len(result.links),
            "attestations_created": len(attestation_ids),
            "attestation_ids": attestation_ids,
        }

        return self._json_response(200, response_data)

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

    def HandleWebSocket(self, request_iterator, context):
        """WebSocket handler (not implemented for this plugin)."""
        # Send close message - webscraper doesn't use WebSocket
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

        return domain_pb2.HealthResponse(
            healthy=healthy,
            message="OK" if healthy else "Not initialized",
            details=details,
        )

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
