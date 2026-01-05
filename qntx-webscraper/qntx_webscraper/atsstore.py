"""ATSStore client for creating attestations via gRPC."""

from __future__ import annotations

import json
import time
from dataclasses import dataclass, field
from typing import Any

import grpc

from .grpc import atsstore_pb2, atsstore_pb2_grpc


@dataclass
class Attestation:
    """Represents an attestation in QNTX."""

    id: str = ""
    subjects: list[str] = field(default_factory=list)
    predicates: list[str] = field(default_factory=list)
    contexts: list[str] = field(default_factory=list)
    actors: list[str] = field(default_factory=list)
    timestamp: int = 0  # Unix timestamp
    source: str = ""
    attributes: dict[str, Any] = field(default_factory=dict)
    created_at: int = 0  # Unix timestamp


@dataclass
class AttestationCommand:
    """Command for creating an attestation with auto-generated ID."""

    subjects: list[str] = field(default_factory=list)
    predicates: list[str] = field(default_factory=list)
    contexts: list[str] = field(default_factory=list)
    actors: list[str] = field(default_factory=list)
    timestamp: int = 0  # Unix timestamp, 0 = now
    attributes: dict[str, Any] = field(default_factory=dict)


@dataclass
class AttestationFilter:
    """Filter for querying attestations."""

    subjects: list[str] = field(default_factory=list)
    predicates: list[str] = field(default_factory=list)
    contexts: list[str] = field(default_factory=list)
    actors: list[str] = field(default_factory=list)
    time_start: int = 0  # Unix timestamp, 0 = no filter
    time_end: int = 0  # Unix timestamp, 0 = no filter
    limit: int = 0  # 0 = no limit


class ATSStoreClient:
    """Client for interacting with QNTX ATSStore via gRPC."""

    def __init__(self, endpoint: str, auth_token: str):
        """Initialize the ATSStore client.

        Args:
            endpoint: gRPC endpoint (e.g., "localhost:50051")
            auth_token: Authentication token for QNTX services
        """
        self.endpoint = endpoint
        self.auth_token = auth_token
        self.channel = grpc.insecure_channel(endpoint)
        self.stub = atsstore_pb2_grpc.ATSStoreServiceStub(self.channel)

    def close(self) -> None:
        """Close the gRPC channel."""
        self.channel.close()

    def __enter__(self) -> ATSStoreClient:
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self.close()

    def create_attestation(self, attestation: Attestation) -> bool:
        """Create an attestation with a pre-generated ID.

        Args:
            attestation: The attestation to create

        Returns:
            True if successful

        Raises:
            RuntimeError: If creation fails
        """
        proto_attestation = atsstore_pb2.Attestation(
            id=attestation.id,
            subjects=attestation.subjects,
            predicates=attestation.predicates,
            contexts=attestation.contexts,
            actors=attestation.actors,
            timestamp=attestation.timestamp or int(time.time()),
            source=attestation.source,
            attributes_json=json.dumps(attestation.attributes) if attestation.attributes else "",
            created_at=attestation.created_at or int(time.time()),
        )

        request = atsstore_pb2.CreateAttestationRequest(
            auth_token=self.auth_token,
            attestation=proto_attestation,
        )

        response = self.stub.CreateAttestation(request)
        if not response.success:
            raise RuntimeError(f"Failed to create attestation: {response.error}")
        return True

    def generate_and_create(self, command: AttestationCommand) -> Attestation:
        """Generate an attestation ID and create the attestation.

        This is the preferred method for creating attestations as QNTX
        will generate a self-certifying ID based on the content.

        Args:
            command: The attestation command with subjects, predicates, etc.

        Returns:
            The created attestation with generated ID

        Raises:
            RuntimeError: If creation fails
        """
        proto_command = atsstore_pb2.AttestationCommand(
            subjects=command.subjects,
            predicates=command.predicates,
            contexts=command.contexts,
            actors=command.actors,
            timestamp=command.timestamp,
            attributes_json=json.dumps(command.attributes) if command.attributes else "",
        )

        request = atsstore_pb2.GenerateAttestationRequest(
            auth_token=self.auth_token,
            command=proto_command,
        )

        response = self.stub.GenerateAndCreateAttestation(request)
        if not response.success:
            raise RuntimeError(f"Failed to generate attestation: {response.error}")

        proto_as = response.attestation
        return Attestation(
            id=proto_as.id,
            subjects=list(proto_as.subjects),
            predicates=list(proto_as.predicates),
            contexts=list(proto_as.contexts),
            actors=list(proto_as.actors),
            timestamp=proto_as.timestamp,
            source=proto_as.source,
            attributes=json.loads(proto_as.attributes_json) if proto_as.attributes_json else {},
            created_at=proto_as.created_at,
        )

    def attestation_exists(self, attestation_id: str) -> bool:
        """Check if an attestation exists.

        Args:
            attestation_id: The attestation ID to check

        Returns:
            True if the attestation exists
        """
        request = atsstore_pb2.AttestationExistsRequest(
            auth_token=self.auth_token,
            id=attestation_id,
        )
        response = self.stub.AttestationExists(request)
        return response.exists

    def get_attestations(self, filter: AttestationFilter) -> list[Attestation]:
        """Query attestations with filters.

        Args:
            filter: The filter criteria

        Returns:
            List of matching attestations

        Raises:
            RuntimeError: If query fails
        """
        proto_filter = atsstore_pb2.AttestationFilter(
            subjects=filter.subjects,
            predicates=filter.predicates,
            contexts=filter.contexts,
            actors=filter.actors,
            time_start=filter.time_start,
            time_end=filter.time_end,
            limit=filter.limit,
        )

        request = atsstore_pb2.GetAttestationsRequest(
            auth_token=self.auth_token,
            filter=proto_filter,
        )

        response = self.stub.GetAttestations(request)
        if not response.success:
            raise RuntimeError(f"Failed to query attestations: {response.error}")

        return [
            Attestation(
                id=proto_as.id,
                subjects=list(proto_as.subjects),
                predicates=list(proto_as.predicates),
                contexts=list(proto_as.contexts),
                actors=list(proto_as.actors),
                timestamp=proto_as.timestamp,
                source=proto_as.source,
                attributes=json.loads(proto_as.attributes_json) if proto_as.attributes_json else {},
                created_at=proto_as.created_at,
            )
            for proto_as in response.attestations
        ]
