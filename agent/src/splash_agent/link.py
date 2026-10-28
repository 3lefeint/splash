"""WebSocket connection to the backend with reconnect and resend."""

import asyncio
import json
import logging
import random

from websockets.asyncio.client import connect
from websockets.exceptions import ConnectionClosed, InvalidHandshake, InvalidStatus

from . import __version__
from .spool import Spool

log = logging.getLogger("splash_agent.link")

MIN_BACKOFF = 1.0
MAX_BACKOFF = 30.0
WAKE_TIMEOUT = 1.0
ACK_TIMEOUT = 10.0


class Link:
    """Sends spooled durable messages in order and the newest live sample of each type."""

    def __init__(self, url: str, token: str, spool: Spool, cf_client_id: str = "", cf_client_secret: str = ""):
        self.url, self.token, self.spool = url, token, spool
        self._cf = (cf_client_id, cf_client_secret) if cf_client_id and cf_client_secret else None
        self._live: dict[str, str] = {}
        self._sleep = asyncio.sleep  # replaced in tests
        self._random = random.random
        self._backlog: tuple[int, int] | None = None  # messages to resend after the connect, and the last seq among them
        self._reached = False  # the current connection got as far as the acknowledged hello
        self._wake = asyncio.Event()
        self.connected = False
        self.on_command = None  # called with each command of the backend

    def notify(self) -> None:
        self._wake.set()

    def push_live(self, type_: str, line: str) -> None:
        self._live[type_] = line
        self._wake.set()

    def _delay(self, backoff: float) -> float:
        """Half of the backoff plus a random part of the other half, so agents that lost the backend together do not
        come back together."""
        return backoff / 2 + self._random() * backoff / 2

    async def run(self) -> None:
        """Connects, and after any loss reconnects with a growing, jittered pause. The pause starts over after a
        connection that worked, so a cut hours after the last one does not wait as long as a long outage does."""
        backoff = MIN_BACKOFF
        attempt = 0
        while True:
            attempt += 1
            self._reached = False
            level = logging.WARNING
            try:
                await self._connection()
                reason = "connection closed"
            except asyncio.CancelledError:
                raise
            except InvalidStatus as e:
                level = logging.ERROR
                reason = f"backend rejected the connection (HTTP {e.response.status_code}); check the tokens"
            except (InvalidHandshake, OSError, ConnectionClosed, asyncio.TimeoutError, ValueError) as e:
                # InvalidHandshake covers a connection that was cut before the HTTP answer, as a restarting tunnel does
                reason = f"connection lost: {e}"
            finally:
                self.connected = False
                self._backlog = None
            if self._reached:
                backoff, attempt = MIN_BACKOFF, 1
            delay = self._delay(backoff)
            log.log(level, "%s; reconnecting in %.1f s (attempt %d)", reason, delay, attempt)
            await self._sleep(delay)
            backoff = min(backoff * 2, MAX_BACKOFF)

    def _headers(self) -> dict[str, str]:
        """Handshake headers: the Splash token and, when configured, the Cloudflare Access service token."""
        headers = {"Authorization": f"Bearer {self.token}"} if self.token else {}
        if self._cf:
            headers["CF-Access-Client-Id"], headers["CF-Access-Client-Secret"] = self._cf
        return headers

    async def _connection(self) -> None:
        headers = self._headers()
        async with connect(self.url, additional_headers=headers, max_size=None) as ws:
            log.debug("handshake ok (HTTP %s)", getattr(getattr(ws, "response", None), "status_code", "?"))
            await self._send(ws, self.spool.hello(__version__))
            acked = _ack_seq(await asyncio.wait_for(ws.recv(), ACK_TIMEOUT))
            self.spool.prune(acked)
            self.connected = True
            self._reached = True
            log.info("connected, backend is at seq %d, %d messages to resend", acked, len(self.spool.entries))
            self._backlog = (len(self.spool.entries), self.spool.entries[-1][0]) if self.spool.entries else None
            sent = acked
            reader = asyncio.create_task(self._read_acks(ws))
            try:
                while True:
                    if reader.done():
                        reader.result()  # raises ConnectionClosed
                        return
                    self._wake.clear()
                    for seq, line in list(self.spool.entries):
                        if seq > sent:
                            await self._send(ws, line)
                            sent = seq
                    live, self._live = self._live, {}
                    for line in live.values():
                        await self._send(ws, line)
                    try:
                        await asyncio.wait_for(self._wake.wait(), WAKE_TIMEOUT)
                    except asyncio.TimeoutError:
                        pass
            finally:
                reader.cancel()

    async def _send(self, ws, line: str) -> None:
        await ws.send(line)
        if log.isEnabledFor(logging.DEBUG):
            msg = json.loads(line)
            log.debug("sent %s seq %s, %d bytes", msg.get("type"), msg.get("seq"), len(line))

    async def _read_acks(self, ws) -> None:
        async for raw in ws:
            msg = json.loads(raw)
            if msg.get("type") == "command":
                if self.on_command is not None:
                    self.on_command(msg)
                continue
            self.spool.prune(_ack_seq(raw))
            if self._backlog and (not self.spool.entries or self.spool.entries[0][0] > self._backlog[1]):
                count, last = self._backlog
                self._backlog = None
                log.info("backlog resent: %d messages up to seq %d, all acknowledged", count, last)


def _ack_seq(raw) -> int:
    msg = json.loads(raw)
    if msg.get("type") != "ack":
        raise ValueError(f"unexpected reply: {msg}")
    return int(msg["seq"])
