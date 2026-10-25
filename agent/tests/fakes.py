"""Builders for plugin buffers and a recording backend, so the agent runs without a game."""

from splash_agent import structs as S
from splash_agent.reader import MappedBuffer


class FakeBuffer(MappedBuffer):
    """A MappedBuffer over a bytearray that tests can overwrite."""

    def __init__(self, struct):
        super().__init__(type(struct), bytearray(bytes(struct)))

    def update(self, struct):
        self.source[:] = bytes(struct)


def _set(obj, values: dict):
    for k, v in values.items():
        setattr(obj, k, v)


def scoring(vehicles=(), track=b"Track A", session=10, start_et=36000.0, et=100.0, end_et=86400.0,
            realtime=True, phase=5, yellow=0, version=2, **info):
    b = S.ScoringBuffer()
    b.mVersionUpdateBegin = b.mVersionUpdateEnd = version
    i = b.mScoringInfo
    _set(i, dict(mTrackName=track, mSession=session, mStartET=start_et, mCurrentET=et, mEndET=end_et,
                 mInRealtime=realtime, mGamePhase=phase, mYellowFlagState=yellow,
                 mNumVehicles=len(vehicles), mServerName=b"server", mAmbientTemp=20.0, mTrackTemp=30.0,
                 mRaining=0.0))
    _set(i, info)
    for n, v in enumerate(vehicles):
        veh = b.mVehicles[n]
        _set(veh, dict(mID=v.get("id", n), mDriverName=v.get("driver", b"driver"),
                       mVehicleName=v.get("name", b"car"), mVehicleClass=v.get("cls", b"GT3"),
                       mPlace=v.get("place", n + 1), mTotalLaps=v.get("laps", 0),
                       mIsPlayer=v.get("player", False), mControl=v.get("control", 1),
                       mInPits=v.get("pits", False), mLastLapTime=v.get("last", -1.0),
                       mLastSector1=v.get("s1", -1.0), mLastSector2=v.get("s12", -1.0),
                       mBestLapTime=-1.0, mTimeBehindNext=v.get("gap", 0.0), mLapDist=v.get("dist", 0.0)))
    return b


def telemetry(vehicles=(), version=2):
    b = S.TelemetryBuffer()
    b.mVersionUpdateBegin = b.mVersionUpdateEnd = version
    b.mNumVehicles = len(vehicles)
    for n, v in enumerate(vehicles):
        t = b.mVehicles[n]
        _set(t, dict(mID=v.get("id", n), mVehicleName=v.get("name", b"car"), mElapsedTime=v.get("et", 100.0),
                     mLapNumber=v.get("lap", 1), mFuel=v.get("fuel", 50.0), mFuelCapacity=v.get("cap", 90.0)))
        for k, w in enumerate(v.get("wear", [0.9, 0.9, 0.9, 0.9])):
            t.mWheels[k].mWear = w
            t.mWheels[k].mTireCarcassTemperature = 273.15 + 80 + k
    return b


class FakeLink:
    """Collects what the agent would send."""

    def __init__(self, spool):
        self.spool = spool
        self.live = {}
        self.notified = 0

    def notify(self):
        self.notified += 1

    def push_live(self, type_, line):
        self.live[type_] = line
