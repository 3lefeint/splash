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


class _View:
    """A read-only view of mapped memory that supports slicing like bytes."""

    def __init__(self, address: int, size: int):
        self.address, self.size = address, size

    def __getitem__(self, item: slice) -> bytes:
        start, stop, _ = item.indices(self.size)
        return C.string_at(self.address + start, max(stop - start, 0))


def open_windows_buffer(cls, tagname: str) -> MappedBuffer:
    """Open an existing named mapping read-only. Only works on Windows.

    Unlike mmap with a tag name this never creates the mapping: if the plugin has not made it yet the
    agent has to wait, otherwise it would read an empty area of its own.
    """
    if sys.platform != "win32":
        raise PluginNotRunning("shared memory is only available on Windows")
    return MappedBuffer(cls, _map_view(tagname, C.sizeof(cls)))


def _map_view(tagname: str, size: int) -> _View:
    return _View(_map_address(tagname, size), size)


def _map_address(tagname: str, size: int, write: bool = False) -> int:
    from ctypes import wintypes as W

    k32 = C.WinDLL("kernel32", use_last_error=True)
    k32.OpenFileMappingW.restype = W.HANDLE
    k32.OpenFileMappingW.argtypes = [W.DWORD, W.BOOL, W.LPCWSTR]
    k32.MapViewOfFile.restype = C.c_void_p
    k32.MapViewOfFile.argtypes = [W.HANDLE, W.DWORD, W.DWORD, W.DWORD, C.c_size_t]
    k32.VirtualQuery.restype = C.c_size_t
    k32.VirtualQuery.argtypes = [C.c_void_p, C.c_void_p, C.c_size_t]

    class MemoryInfo(C.Structure):
        _fields_ = [("BaseAddress", C.c_void_p), ("AllocationBase", C.c_void_p), ("AllocationProtect", W.DWORD),
                    ("PartitionId", W.WORD), ("RegionSize", C.c_size_t), ("State", W.DWORD),
                    ("Protect", W.DWORD), ("Type", W.DWORD)]

    file_map_read = 0x0004
    access = 0x0006 if write else file_map_read  # FILE_MAP_WRITE | FILE_MAP_READ
    handle = k32.OpenFileMappingW(access, False, tagname)
    if not handle:
        raise PluginNotRunning(f"{tagname}: Windows error {C.get_last_error()}")
    address = k32.MapViewOfFile(handle, access, 0, 0, 0)  # 0 maps the whole object
    if not address:
        raise PluginNotRunning(f"{tagname}: cannot map the view (Windows error {C.get_last_error()})")
    info = MemoryInfo()
    if k32.VirtualQuery(address, C.byref(info), C.sizeof(info)) and info.RegionSize < size:
        raise PluginNotRunning(
            f"{tagname}: the plugin buffer is {info.RegionSize} bytes, the agent expects {size}; "
            "plugin and agent versions do not match")
    return address


class HWControl:
    """Writes named controls (``PitMenuUp``, ...) into the plugin's input buffer."""

    LAYOUT_VERSION = 1

    def __init__(self, buf: S.HWControlBuffer):
        self.buf = buf

    def send(self, name: str, down: bool) -> None:
        b = self.buf
        b.mVersionUpdateBegin = (b.mVersionUpdateBegin + 1) & 0xFFFFFFFF
        b.mLayoutVersion = self.LAYOUT_VERSION
        b.mControlName = name.encode("ascii")
        b.mfRetVal = 1.0 if down else 0.0
        b.mVersionUpdateEnd = b.mVersionUpdateBegin


def open_hw_control() -> HWControl:
    """Open the plugin's HWControl input buffer for writing. Only works on Windows while the game runs."""
    if sys.platform != "win32":
        raise PluginNotRunning("shared memory is only available on Windows")
    address = _map_address(S.HW_CONTROL_FILE, C.sizeof(S.HWControlBuffer), write=True)
    return HWControl(S.HWControlBuffer.from_address(address))
