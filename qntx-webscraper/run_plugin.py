#!/usr/bin/env python3
"""Run the QNTX webscraper plugin as a standalone gRPC server."""

import argparse
import logging
import signal
import sys
from concurrent import futures

import grpc

from qntx_webscraper.grpc import domain_pb2_grpc
from qntx_webscraper.plugin import WebScraperPlugin

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


def serve(port: int = 50052):
    """Start the gRPC server."""
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    plugin = WebScraperPlugin()
    domain_pb2_grpc.add_DomainPluginServiceServicer_to_server(plugin, server)

    server.add_insecure_port(f'[::]:{port}')
    server.start()

    logger.info(f"QNTX WebScraper plugin server started on port {port}")
    logger.info("Press Ctrl+C to stop")

    # Handle shutdown gracefully
    def shutdown_handler(signum, frame):
        logger.info("Shutting down...")
        server.stop(grace=5)
        sys.exit(0)

    signal.signal(signal.SIGINT, shutdown_handler)
    signal.signal(signal.SIGTERM, shutdown_handler)

    # Keep the server running
    server.wait_for_termination()


def main():
    parser = argparse.ArgumentParser(description='Run QNTX WebScraper plugin')
    parser.add_argument(
        '--port',
        type=int,
        default=50052,
        help='Port to listen on (default: 50052)'
    )
    parser.add_argument(
        '--log-level',
        choices=['DEBUG', 'INFO', 'WARNING', 'ERROR'],
        default='INFO',
        help='Logging level (default: INFO)'
    )

    args = parser.parse_args()

    # Set logging level
    logging.getLogger().setLevel(getattr(logging, args.log_level))

    serve(args.port)


if __name__ == '__main__':
    main()