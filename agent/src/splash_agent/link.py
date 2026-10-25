"""WebSocket connection to the backend with reconnect and resend."""

import asyncio
import json
import logging

from websockets.asyncio.client import connect
from websockets.exceptions import ConnectionClosed, InvalidStatus

from . import __version__
from .spool import Spool

log = logging.getLogger("splash_agent.link")

MIN_BACKOFF = 1.0
MAX_BACKOFF = 30.0
WAKE_TIMEOUT = 1.0
ACK_TIMEOUT = 10.0


class Link:
    """Sends spooled durable messages in order and the newest live sample of each type."""

    def __init__(self, url: str, token: str, spool: Spool):
        self.url, self.token, self.spool = url, token, spool
        self._live: dict[str, str] = {}
        self._wake = asyncio.Event()
        self.connected = False
        self.on_command = None  # called with each command of the backend

    def notify(self) -> None:
        self._wake.set()

    def push_live(self, type_: str, line: str) -> None:
        self._live[type_] = line
        self._wake.set()

    async def run(self) -> None:
        backoff = MIN_BACKOFF
        while True:
            try:
                await self._connection()
                backoff = MIN_BACKOFF
            except asyncio.CancelledError:
                raise
            except InvalidStatus as e:
                log.error("backend rejected the connection (HTTP %s); check the token", e.response.status_code)
            except (OSError, ConnectionClosed, asyncio.TimeoutError) as e:
                log.warning("connection lost: %s", e)
            finally:
                self.connected = False
            await asyncio.sleep(backoff)
            backoff = min(backoff * 2, MAX_BACKOFF)

    async def _connection(self) -> None:
        headers = {"Authorization": f"Bearer {self.token}"} if self.token else {}
        async with connect(self.url, additional_headers=headers, max_size=None) as ws:
            await ws.send(self.spool.hello(__version__))
            acked = _ack_seq(await asyncio.wait_for(ws.recv(), ACK_TIMEOUT))
            self.spool.prune(acked)
            self.connected = True
            log.info("connected, backend is at seq %d, %d messages to resend", acked, len(self.spool.entries))
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
                            await ws.send(line)
                            sent = seq
                    live, self._live = self._live, {}
                    for line in live.values():
                        await ws.send(line)
                    try:
                        await asyncio.wait_for(self._wake.wait(), WAKE_TIMEOUT)
                    except asyncio.TimeoutError:
                        pass
            finally:
                reader.cancel()

    async def _read_acks(self, ws) -> None:
        async for raw in ws:
            msg = json.loads(raw)
            if msg.get("type") == "command":
                if self.on_command is not None:
                    self.on_command(msg)
                continue
            self.spool.prune(_ack_seq(raw))


def _ack_seq(raw) -> int:
    msg = json.loads(raw)
    if msg.get("type") != "ack":
        raise ValueError(f"unexpected reply: {msg}")
    return int(msg["seq"])
