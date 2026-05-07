"""Pulsar publisher with JWT auth and TLS, KeyBasedBatching.

Mirrors the Go PulsarFirer pattern from internal/demofire/firer_pulsar.go:
  - KeyBasedBatchBuilder for partition affinity
  - message key = anonymousId
  - properties: writeKey, sourceId, messageId
  - JWT bearer + optional TLS CA cert

Thread-safety: Each worker thread should construct its own PulsarPublisher
within a `with` block. Producer names are unique per instance to avoid
Pulsar's ProducerBusy error when concurrent producers share a topic.
"""
from __future__ import annotations

import json
import logging
from types import TracebackType
from typing import Any

import pulsar

logger = logging.getLogger(__name__)


class PulsarPublisher:
    """Publish RudderStack events to a Pulsar topic.

    Usage::

        with PulsarPublisher(url, topic, jwt_token, tls_trust_certs=...) as pub:
            msg_id = pub.publish(event_dict, write_key="3DNy...")
    """

    def __init__(
        self,
        url: str,
        topic: str,
        jwt_token: str,
        tls_trust_certs: str | None = None,
        tls_validate_hostname: bool = True,
        tls_allow_insecure: bool = False,
        producer_name: str | None = None,
    ) -> None:
        import uuid as _uuid
        self._url = url
        self._topic = topic
        self._jwt_token = jwt_token
        self._tls_trust_certs = tls_trust_certs
        self._tls_validate_hostname = tls_validate_hostname
        self._tls_allow_insecure = tls_allow_insecure
        # Default to a unique name per instance so concurrent producers don't collide.
        self._producer_name = producer_name or f"demo-events-py-{_uuid.uuid4().hex[:8]}"
        self._client: pulsar.Client | None = None
        self._producer: pulsar.Producer | None = None

    # ------------------------------------------------------------------
    # Context manager
    # ------------------------------------------------------------------

    def __enter__(self) -> "PulsarPublisher":
        self._connect()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        self.close()

    # ------------------------------------------------------------------
    # Connection management
    # ------------------------------------------------------------------

    def _connect(self) -> None:
        """Create the Pulsar client and producer (lazy; called by __enter__)."""
        client_opts: dict[str, Any] = {
            "authentication": pulsar.AuthenticationToken(self._jwt_token),
            "operation_timeout_seconds": 30,
        }

        if self._tls_trust_certs:
            client_opts["tls_trust_certs_file_path"] = self._tls_trust_certs
        client_opts["tls_allow_insecure_connection"] = self._tls_allow_insecure
        client_opts["tls_validate_hostname"] = self._tls_validate_hostname

        self._client = pulsar.Client(self._url, **client_opts)

        self._producer = self._client.create_producer(
            self._topic,
            producer_name=self._producer_name,
            batching_type=pulsar.BatchingType.KeyBased,
            block_if_queue_full=True,
            send_timeout_millis=30000,
        )
        logger.debug("PulsarPublisher connected: topic=%s", self._topic)

    # ------------------------------------------------------------------
    # Publishing
    # ------------------------------------------------------------------

    def publish(
        self,
        event: dict[str, Any],
        write_key: str,
        source_id: str | None = None,
    ) -> str:
        """Publish one event and return the Pulsar message_id as a string.

        Sets:
          payload          = json.dumps(event)
          partition_key    = event['anonymousId']
          properties       = {writeKey, sourceId, messageId}
        """
        if self._producer is None:
            raise RuntimeError("PulsarPublisher is not connected — use as context manager or call _connect()")

        payload = json.dumps(event, default=str).encode("utf-8")
        anon_id: str = event.get("anonymousId", "")
        message_id_str: str = event.get("messageId", "")
        effective_source = source_id or write_key

        msg_id = self._producer.send(
            payload,
            partition_key=anon_id,
            properties={
                "writeKey":  write_key,
                "sourceId":  effective_source,
                "messageId": message_id_str,
            },
        )

        logger.debug(
            "published: type=%s event=%s anon=%s msgId=%s",
            event.get("type"),
            event.get("event", ""),
            anon_id,
            message_id_str,
        )
        return str(msg_id)

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------

    def flush(self) -> None:
        """Flush pending batched messages."""
        if self._producer is not None:
            try:
                self._producer.flush()
            except Exception as exc:  # noqa: BLE001
                logger.warning("PulsarPublisher: flush error: %s", exc)

    def close(self) -> None:
        """Flush, close producer, then close client."""
        self.flush()
        if self._producer is not None:
            try:
                self._producer.close()
            except Exception as exc:  # noqa: BLE001
                logger.warning("PulsarPublisher: producer close error: %s", exc)
            self._producer = None
        if self._client is not None:
            try:
                self._client.close()
            except Exception as exc:  # noqa: BLE001
                logger.warning("PulsarPublisher: client close error: %s", exc)
            self._client = None
