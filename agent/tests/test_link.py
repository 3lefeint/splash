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
        self.all_headers = []
        self.drop_first = drop_first_connection
        self.connections = 0

    async def handler(self, ws):
        self.connections += 1
        self.headers.append(ws.request.headers.get("Authorization"))
        self.all_headers.append(ws.request.headers)
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
    s = Spool(tmp_path, "driver-pc")
    s.open("sess")
    for i in range(n):
        s.append("lap", float(i), {"car_id": 0, "vehicle_name": "v", "lap": i + 1, "in_lap": False, "out_lap": False})
    return s


def run_link(backend, spool, token="secret", scenario=None, **link_kwargs):
    async def main():
        async with serve(backend.handler, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            link = Link(f"ws://127.0.0.1:{port}", token, spool, **link_kwargs)
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


def _connect_once(tmp_path, **link_kwargs):
    backend = Backend()

    async def scenario(link):
        await until(lambda: link.connected)

    run_link(backend, make_spool(tmp_path), scenario=scenario, **link_kwargs)
    return backend.all_headers[0]


def test_sends_the_access_service_token_only_when_both_values_are_set(tmp_path):
    h = _connect_once(tmp_path, cf_client_id="id.access", cf_client_secret="cf-secret-value")
    assert h["CF-Access-Client-Id"] == "id.access"
    assert h["CF-Access-Client-Secret"] == "cf-secret-value"
    assert h["Authorization"] == "Bearer secret"  # the Splash token is sent in addition

    for kwargs in ({}, {"cf_client_id": "id.access"}, {"cf_client_secret": "cf-secret-value"}):
        h = _connect_once(tmp_path, **kwargs)
        assert "CF-Access-Client-Id" not in h and "CF-Access-Client-Secret" not in h
        assert h["Authorization"] == "Bearer secret"


def test_the_access_secret_is_never_logged(tmp_path, caplog):
    # The agent logs at INFO (main.py). The websockets library would print every handshake header at DEBUG, the
    # Splash token included, so the agent must never be run with that level.
    caplog.set_level("INFO")
    caplog.set_level("DEBUG", logger="splash_agent")
    _connect_once(tmp_path, cf_client_id="id.access", cf_client_secret="cf-secret-value")
    assert "cf-secret-value" not in caplog.text


def _run_with_delays(backend, spool, scenario, **kw):
    """Runs a Link whose pauses are recorded instead of waited for in full."""
    delays = []

    async def main():
        async with serve(backend.handler, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            link = Link(f"ws://127.0.0.1:{port}", "secret", spool, **kw)
            link._random = lambda: 1.0  # the longest pause, to make the numbers exact

            async def sleep(d):
                delays.append(d)
                await asyncio.sleep(0.01)

            link._sleep = sleep
            task = asyncio.create_task(link.run())
            try:
                await scenario(link)
            finally:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)

    asyncio.run(main())
    return delays


class DroppingBackend(Backend):
    """Closes every connection right after the hello, as a tunnel restart or an idle timeout would."""

    def __init__(self, drops):
        super().__init__()
        self.drops = drops

    async def handler(self, ws):
        self.connections += 1
        async for raw in ws:
            msg = json.loads(raw)
            self.received.append(msg)
            await ws.send(json.dumps({"type": "ack", "seq": self.last}))
            if self.connections <= self.drops:
                await ws.close()
                return


def test_backoff_starts_over_after_a_connection_that_worked(tmp_path):
    backend = DroppingBackend(drops=3)

    async def scenario(link):
        await until(lambda: backend.connections >= 4)
        await asyncio.sleep(0.05)

    delays = _run_with_delays(backend, make_spool(tmp_path), scenario)
    # every dropped connection had been acknowledged, so the pause is the first step each time and never doubles
    assert delays[:3] == [linkmod.MIN_BACKOFF] * 3


def test_pause_grows_with_jitter_while_the_backend_stays_away(tmp_path):
    spool = make_spool(tmp_path)
    delays = []

    async def main():
        link = Link("ws://127.0.0.1:1", "secret", spool)  # nothing listens there
        values = iter([0.0, 0.5, 1.0, 0.0])
        link._random = lambda: next(values)

        async def sleep(d):
            delays.append(d)
            if len(delays) == 4:
                raise asyncio.CancelledError
            await asyncio.sleep(0)

        link._sleep = sleep
        await asyncio.gather(link.run(), return_exceptions=True)

    asyncio.run(main())
    b = linkmod.MIN_BACKOFF
    assert delays == [b / 2, 0.75 * 2 * b, 4 * b, 4 * b]  # jitter between half and the full step, steps double


def test_a_connection_cut_before_the_http_answer_does_not_end_the_link(tmp_path, caplog):
    caplog.set_level("INFO")
    attempts = []

    async def cut(reader, writer):
        attempts.append(1)
        writer.close()  # no HTTP answer at all: websockets raises InvalidMessage

    async def main():
        server = await asyncio.start_server(cut, "127.0.0.1", 0)
        port = server.sockets[0].getsockname()[1]
        link = Link(f"ws://127.0.0.1:{port}", "secret", make_spool(tmp_path), "id.access", "cf-secret-value")
        sleeps = []

        async def sleep(d):
            sleeps.append(d)
            await asyncio.sleep(0.01)

        link._sleep = sleep
        task = asyncio.create_task(link.run())
        await until(lambda: len(attempts) >= 3)
        assert not task.done(), "the link must keep trying"
        task.cancel()
        await asyncio.gather(task, return_exceptions=True)
        server.close()

    asyncio.run(main())
    lines = [r.getMessage() for r in caplog.records if r.name == "splash_agent.link"]
    assert len(lines) >= 2 and all("reconnecting in" in l and "attempt" in l for l in lines)
    assert not any("secret" in l.lower().replace("cf-access", "") for l in lines)  # no token in any line


def test_one_log_line_per_attempt_without_secrets(tmp_path, caplog):
    caplog.set_level("INFO")
    backend = DroppingBackend(drops=2)

    async def scenario(link):
        await until(lambda: backend.connections >= 3)

    _run_with_delays(backend, make_spool(tmp_path), scenario, cf_client_id="id.access", cf_client_secret="cf-secret-value")
    lines = [r.getMessage() for r in caplog.records if r.name == "splash_agent.link" and "reconnecting" in r.getMessage()]
    assert len(lines) >= 2
    assert all(l.count("reconnecting in") == 1 for l in lines)
    assert "cf-secret-value" not in caplog.text and "Bearer" not in caplog.text
