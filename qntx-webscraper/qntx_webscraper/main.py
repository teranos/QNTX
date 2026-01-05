"""Main entry point for the QNTX webscraper plugin."""

from __future__ import annotations

import argparse
import logging
import signal
import sys

from .plugin import serve


def main():
    """Run the webscraper plugin as a gRPC server."""
    parser = argparse.ArgumentParser(
        description="QNTX Webscraper Plugin - Extract URLs and create attestations"
    )
    parser.add_argument(
        "--port",
        type=int,
        default=9001,
        help="Port to listen on (default: 9001)",
    )
    parser.add_argument(
        "--log-level",
        choices=["DEBUG", "INFO", "WARNING", "ERROR"],
        default="INFO",
        help="Logging level (default: INFO)",
    )

    args = parser.parse_args()

    # Configure logging
    logging.basicConfig(
        level=getattr(logging, args.log_level),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )

    logger = logging.getLogger(__name__)
    logger.info(f"Starting QNTX webscraper plugin on port {args.port}")

    # Start the server
    server = serve(args.port)

    # Handle shutdown signals
    def shutdown(signum, frame):
        logger.info("Received shutdown signal, stopping server...")
        server.stop(grace=5)
        sys.exit(0)

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)

    # Wait for server to finish
    server.wait_for_termination()


if __name__ == "__main__":
    main()
