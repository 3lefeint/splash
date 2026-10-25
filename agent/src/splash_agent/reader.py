"""Consistent, read-only access to the mapped plugin buffers."""

import ctypes as C
import mmap
import sys
from typing import Protocol

from . import structs as S

_VERSION_SIZE = 8  # two uint32 counters in front of every buffer


class Source(Protocol):
    """Anything that supports slicing like bytes: an mmap or a bytearray."""

    def __getitem__(self, item: slice) -> bytes: ...


class MappedBuffer:
    """Reads one plugin buffer and returns a copy that is not torn by a concurrent write."""

    def __init__(self, cls, source: Source, retries: int = 3):
        self.cls = cls
        self.source = source
        self.retries = retries
        self.size = C.sizeof(cls)

    def read(self, prefix: int | None = None):
        """Return a copy of the buffer, or None if no consistent view could be read.

        ``prefix`` limits the copy to the first bytes of the buffer (the rest stays
        zero), which saves most of the work when only the first vehicles are needed.
        """
        n = self.size if prefix is None else min(prefix, self.size)
        for _ in range(self.retries):
            data = bytes(self.source[0:n])
            begin, end = _versions(data)
            if begin != end:
                continue  # the plugin was writing; try again
            if _versions(bytes(self.source[0:_VERSION_SIZE])) != (begin, end):
                continue  # changed while copying
            buf = bytearray(self.size)
            buf[:n] = data
            return self.cls.from_buffer_copy(buf)
        return None


def _versions(data: bytes) -> tuple[int, int]:
    return int.from_bytes(data[0:4], "little"), int.from_bytes(data[4:8], "little")


def telemetry_prefix(vehicles: int) -> int:
    return S.TelemetryBuffer.mVehicles.offset + vehicles * C.sizeof(S.VehicleTelemetry)


def scoring_prefix(vehicles: int) -> int:
    return S.ScoringBuffer.mVehicles.offset + vehicles * C.sizeof(S.VehicleScoring)


class PluginNotRunning(RuntimeError):
    """The mapped file does not exist (game not running or plugin not enabled)."""


def open_windows_buffer(cls, tagname: str) -> MappedBuffer:
    """Open an existing named mapping read-only. Only works on Windows."""
    if sys.platform != "win32":
        raise PluginNotRunning("shared memory is only available on Windows")
    try:
        m = mmap.mmap(-1, C.sizeof(cls), tagname=tagname, access=mmap.ACCESS_READ)
    except OSError as e:
        raise PluginNotRunning(f"{tagname}: {e}") from e
    return MappedBuffer(cls, m)
