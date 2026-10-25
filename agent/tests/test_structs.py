import ctypes as C

import pytest

from splash_agent import structs as S

# Sizes and offsets of the plugin header (pack 4, 32-bit long, 64-bit build), taken from a compiler run over the
# original structs. Every field of the ctypes mirrors was compared the same way when they were written.
EXPECTED = {
    S.Wheel: {"": 260, "mPressure": 120, "mTemperature": 128, "mWear": 152, "mTireCarcassTemperature": 204},
    S.VehicleTelemetry: {"": 1888, "mID": 0, "mElapsedTime": 12, "mLapNumber": 20, "mVehicleName": 32,
                         "mFuel": 524, "mFuelCapacity": 608, "mWheels": 848},
    S.ScoringInfo: {"": 548, "mTrackName": 0, "mSession": 64, "mCurrentET": 68, "mEndET": 76, "mNumVehicles": 104,
                    "mGamePhase": 108, "mYellowFlagState": 109, "mSectorFlag": 110, "mInRealtime": 115,
                    "mRaining": 220, "mAmbientTemp": 228, "mTrackTemp": 236, "mServerName": 296, "mStartET": 328},
    S.VehicleScoring: {"": 584, "mID": 0, "mDriverName": 4, "mVehicleName": 36, "mTotalLaps": 100,
                       "mLastLapTime": 168, "mNumPitstops": 192, "mIsPlayer": 196, "mControl": 197, "mInPits": 198,
                       "mPlace": 199, "mVehicleClass": 200, "mTimeBehindNext": 232, "mLapsBehindLeader": 252,
                       "mPitState": 457, "mLapDist": 104},
    # The mapped file starts with the 8 byte version block.
    S.TelemetryBuffer: {"": 8 + 241672, "mBytesUpdatedHint": 8, "mNumVehicles": 12, "mVehicles": 16},
    S.ScoringBuffer: {"": 8 + 75304, "mBytesUpdatedHint": 8, "mScoringInfo": 12, "mVehicles": 560},
}


@pytest.mark.parametrize("cls", EXPECTED, ids=lambda c: c.__name__)
def test_layout(cls):
    for field, want in EXPECTED[cls].items():
        got = C.sizeof(cls) if field == "" else getattr(cls, field).offset
        assert got == want, f"{cls.__name__}.{field or 'size'}"
