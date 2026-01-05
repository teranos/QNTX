"""Pulse (꩜) client for scheduling async jobs via gRPC."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

import grpc

from .grpc import queue_pb2, queue_pb2_grpc


@dataclass
class JobProgress:
    """Progress of a job."""

    current: int = 0
    total: int = 0


@dataclass
class Job:
    """Represents an async job in the Pulse queue."""

    id: str = ""
    handler_name: str = ""
    payload: dict[str, Any] = field(default_factory=dict)
    source: str = ""
    status: str = "queued"  # queued, running, paused, completed, failed, cancelled
    progress: JobProgress = field(default_factory=JobProgress)
    error: str = ""
    parent_job_id: str = ""
    retry_count: int = 0
    created_at: int = 0
    started_at: int = 0
    completed_at: int = 0


class PulseClient:
    """Client for interacting with QNTX Pulse (꩜) queue via gRPC."""

    # Handler names for webscraper jobs
    HANDLER_SCRAPE = "webscraper.scrape"
    HANDLER_SCRAPE_FEED = "webscraper.scrape-feed"
    HANDLER_SCRAPE_SITEMAP = "webscraper.scrape-sitemap"
    HANDLER_CRAWL = "webscraper.crawl"

    def __init__(self, endpoint: str, auth_token: str):
        """Initialize the Pulse client.

        Args:
            endpoint: gRPC endpoint (e.g., "localhost:50052")
            auth_token: Authentication token for QNTX services
        """
        self.endpoint = endpoint
        self.auth_token = auth_token
        self.channel = grpc.insecure_channel(endpoint)
        self.stub = queue_pb2_grpc.QueueServiceStub(self.channel)

    def close(self) -> None:
        """Close the gRPC channel."""
        self.channel.close()

    def __enter__(self) -> PulseClient:
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self.close()

    def enqueue(self, job: Job) -> str:
        """Enqueue a new job.

        Args:
            job: The job to enqueue

        Returns:
            The created job ID

        Raises:
            RuntimeError: If enqueue fails
        """
        proto_job = queue_pb2.Job(
            handler_name=job.handler_name,
            payload=json.dumps(job.payload).encode(),
            source=job.source,
            status=job.status,
            progress=queue_pb2.Progress(
                current=job.progress.current,
                total=job.progress.total,
            ),
            parent_job_id=job.parent_job_id,
        )

        request = queue_pb2.EnqueueRequest(
            auth_token=self.auth_token,
            job=proto_job,
        )

        response = self.stub.Enqueue(request)
        if not response.success:
            raise RuntimeError(f"Failed to enqueue job: {response.error}")
        return response.job_id

    def get_job(self, job_id: str) -> Job:
        """Get a job by ID.

        Args:
            job_id: The job ID

        Returns:
            The job

        Raises:
            RuntimeError: If job not found
        """
        request = queue_pb2.GetJobRequest(
            auth_token=self.auth_token,
            job_id=job_id,
        )

        response = self.stub.GetJob(request)
        if not response.success:
            raise RuntimeError(f"Failed to get job: {response.error}")

        return self._proto_to_job(response.job)

    def list_jobs(self, status: str = "", limit: int = 100) -> list[Job]:
        """List jobs with optional status filter.

        Args:
            status: Optional status filter
            limit: Maximum results

        Returns:
            List of jobs

        Raises:
            RuntimeError: If list fails
        """
        request = queue_pb2.ListJobsRequest(
            auth_token=self.auth_token,
            status=status,
            limit=limit,
        )

        response = self.stub.ListJobs(request)
        if not response.success:
            raise RuntimeError(f"Failed to list jobs: {response.error}")

        return [self._proto_to_job(j) for j in response.jobs]

    def _proto_to_job(self, proto: queue_pb2.Job) -> Job:
        """Convert proto Job to dataclass."""
        payload = {}
        if proto.payload:
            try:
                payload = json.loads(proto.payload.decode())
            except (json.JSONDecodeError, UnicodeDecodeError):
                pass

        return Job(
            id=proto.id,
            handler_name=proto.handler_name,
            payload=payload,
            source=proto.source,
            status=proto.status,
            progress=JobProgress(
                current=proto.progress.current if proto.progress else 0,
                total=proto.progress.total if proto.progress else 0,
            ),
            error=proto.error,
            parent_job_id=proto.parent_job_id,
            retry_count=proto.retry_count,
            created_at=proto.created_at,
            started_at=proto.started_at,
            completed_at=proto.completed_at,
        )

    # ==================== Convenience Methods ====================

    def schedule_scrape(
        self,
        url: str,
        actor: str = "",
        extract_all: bool = True,
    ) -> str:
        """Schedule a URL scrape job.

        Args:
            url: The URL to scrape
            actor: The actor for attestations
            extract_all: Whether to extract all metadata

        Returns:
            The job ID
        """
        job = Job(
            handler_name=self.HANDLER_SCRAPE,
            payload={
                "url": url,
                "actor": actor,
                "extract_all": extract_all,
            },
            source="qntx-webscraper",
        )
        return self.enqueue(job)

    def schedule_feed_scrape(
        self,
        url: str,
        actor: str = "",
    ) -> str:
        """Schedule a feed scrape job.

        Args:
            url: The feed URL
            actor: The actor for attestations

        Returns:
            The job ID
        """
        job = Job(
            handler_name=self.HANDLER_SCRAPE_FEED,
            payload={
                "url": url,
                "actor": actor,
            },
            source="qntx-webscraper",
        )
        return self.enqueue(job)

    def schedule_sitemap_scrape(
        self,
        url: str,
        actor: str = "",
        follow_nested: bool = True,
    ) -> str:
        """Schedule a sitemap scrape job.

        Args:
            url: The sitemap URL
            actor: The actor for attestations
            follow_nested: Whether to follow nested sitemaps

        Returns:
            The job ID
        """
        job = Job(
            handler_name=self.HANDLER_SCRAPE_SITEMAP,
            payload={
                "url": url,
                "actor": actor,
                "follow_nested": follow_nested,
            },
            source="qntx-webscraper",
        )
        return self.enqueue(job)

    def schedule_crawl(
        self,
        url: str,
        actor: str = "",
        max_pages: int = 10,
        same_domain_only: bool = True,
    ) -> str:
        """Schedule a crawl job.

        Args:
            url: The starting URL
            actor: The actor for attestations
            max_pages: Maximum pages to crawl
            same_domain_only: Only follow same-domain links

        Returns:
            The job ID
        """
        job = Job(
            handler_name=self.HANDLER_CRAWL,
            payload={
                "url": url,
                "actor": actor,
                "max_pages": max_pages,
                "same_domain_only": same_domain_only,
            },
            source="qntx-webscraper",
        )
        return self.enqueue(job)
