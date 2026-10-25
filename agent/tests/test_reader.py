from splash_agent import reader, structs as S

from . import fakes


def test_consistent_read():
    buf = fakes.FakeBuffer(fakes.scoring([{"driver": b"abc"}]))
    got = buf.read()
    assert got is not None and got.mVehicles[0].mDriverName == b"abc"


def test_torn_write_is_not_returned():
    s = fakes.scoring([{}])
    s.mVersionUpdateBegin, s.mVersionUpdateEnd = 5, 4  # plugin is in the middle of a write
    assert fakes.FakeBuffer(s).read() is None


def test_prefix_read_only_copies_the_start():
    t = fakes.telemetry([{"id": 7, "fuel": 12.5}, {"id": 8, "fuel": 99.0}])
    buf = fakes.FakeBuffer(t)
    got = buf.read(prefix=reader.telemetry_prefix(1))
    assert got.mVehicles[0].mFuel == 12.5
    assert got.mVehicles[1].mFuel == 0.0  # beyond the prefix


def test_version_changing_while_copying_retries(monkeypatch):
    buf = fakes.FakeBuffer(fakes.scoring([{}]))
    real = reader._versions
    calls = {"n": 0}

    def flaky(data):
        calls["n"] += 1
        # the re-check after the copy (2nd call) sees a different version: the copy was torn
        return (9, 9) if calls["n"] == 2 else real(data)

    monkeypatch.setattr(reader, "_versions", flaky)
    assert buf.read() is not None
    assert calls["n"] == 4  # two calls per attempt, the second attempt succeeded
