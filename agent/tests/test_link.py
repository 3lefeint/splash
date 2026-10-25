import asyncio
import json

from websockets.asyncio.server import serve

from splash_agent import link as linkmod
from splash_agent.link import Link
from splash_agent.spool import Spool


class Backend:
    """A minimal stand-in for the backend's agent socket: acks the highest durable seq it stored."""

    def __init__(self, last=0, drop_first_connection=False):
        self.last = last
        self.received = []
        self.headers = []
        self.drop_first = drop_first_connection
        self.connections = 0

    async def handler(self, ws):
        self.connections += 1
        self.headers.append(ws.request.headers.get("Authorization"))
        async for raw in ws:
            msg = json.loads(raw)
            self.received.append(msg)
            if self.drop_first and self.connections == 1 and msg["type"] == "hello":
                await ws.close()
                return
            if msg["type"] in ("lap", "event") and msg["seq"] > self.last:
                self.last = msg["seq"]
            await ws.send(json.dumps({"type": "ack", "seq": self.last}))


async def until(cond, timeout=5.0):
    end = asyncio.get_running_loop().time() + timeout
    while not cond():
        if asyncio.get_running_loop().time() > end:
            raise AssertionError("condition not met in time")
        await asyncio.sleep(0.01)


def make_spool(tmp_path, n=0):
    s = Spool(tmp_path, "driver-pc", "driver")
    s.open("sess")
    for i in range(n):
        s.append("lap", float(i), {"car_id": 0, "vehicle_name": "v", "lap": i + 1, "in_lap": False, "out_lap": False})
    return s


def run_link(backend, spool, token="secret", scenario=None):
    async def main():
        async with serve(backend.handler, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            link = Link(f"ws://127.0.0.1:{port}", token, spool)
            task = asyncio.create_task(link.run())
            try:
                await scenario(link)
            finally:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)

    asyncio.run(main())


def test_hello_first_with_token_then_only_unacknowledged_messages(tmp_path):
    spool = make_spool(tmp_path, 3)
    backend = Backend(last=1)  # the backend already has lap 1

    async def scenario(link):
        await until(lambda: backend.last == 3)
        await until(lambda: not spool.entries)

    run_link(backend, spool, scenario=scenario)
    assert backend.headers == ["Bearer secret"]
    assert [m["type"] for m in backend.received] == ["hello", "lap", "lap"]
    assert [m["seq"] for m in backend.received] == [0, 2, 3]
    assert backend.received[0]["payload"]["resume_from_seq"] == 1


def test_new_durable_message_is_sent_and_pruned_on_ack(tmp_path):
    spool = make_spool(tmp_path)
    backend = Backend()

    async def scenario(link):
        await until(lambda: link.connected)
        seq, _ = spool.append("lap", 5.0, {"car_id": 0, "vehicle_name": "v", "lap": 1, "in_lap": False, "out_lap": False})
        link.notify()
        await until(lambda: backend.last == seq and not spool.entries)

    run_link(backend, spool, scenario=scenario)


def test_live_samples_are_sent_but_only_the_newest_per_type(tmp_path):
    spool = make_spool(tmp_path)
    backend = Backend()

    async def scenario(link):
        await until(lambda: link.connected)
        for fuel in (50, 49, 48):  # produced faster than they can be sent
            link.push_live("telemetry", spool.live("telemetry", float(fuel), {"fuel": fuel}))
        await until(lambda: any(m["type"] == "telemetry" and m["payload"]["fuel"] == 48 for m in backend.received))

    run_link(backend, spool, scenario=scenario)


def test_reconnects_and_resends_after_the_connection_dropped(tmp_path, monkeypatch):
    monkeypatch.setattr(linkmod, "MIN_BACKOFF", 0.05)
    spool = make_spool(tmp_path, 2)
    backend = Backend(drop_first_connection=True)

    async def scenario(link):
        await until(lambda: backend.last == 2 and backend.connections == 2)

    run_link(backend, spool, scenario=scenario)
    assert [m["seq"] for m in backend.received if m["type"] == "lap"] == [1, 2]
