"""ctypes mirrors of the rFactor 2 Shared Memory Map Plugin buffers.

The layout follows the plugin header: 4-byte packing, 32-bit ``long``, 64-bit
build (pointer fields are 8 bytes). In the mapped file the buffer is preceded by
a version block (two 32-bit counters). Every struct is declared completely so
that offsets stay correct; the agent only reads the fields it needs.
"""

import ctypes as C

MAX_MAPPED_VEHICLES = 128

# File names of the mapped buffers.
TELEMETRY_FILE = "$rFactor2SMMP_Telemetry$"
SCORING_FILE = "$rFactor2SMMP_Scoring$"
PIT_INFO_FILE = "$rFactor2SMMP_PitInfo$"



class _Packed(C.Structure):
    _pack_ = 4


class Vec3(_Packed):
    _fields_ = [("x", C.c_double), ("y", C.c_double), ("z", C.c_double)]


class Wheel(_Packed):
    _fields_ = [
        ("mSuspensionDeflection", C.c_double),
        ("mRideHeight", C.c_double),
        ("mSuspForce", C.c_double),
        ("mBrakeTemp", C.c_double),
        ("mBrakePressure", C.c_double),
        ("mRotation", C.c_double),
        ("mLateralPatchVel", C.c_double),
        ("mLongitudinalPatchVel", C.c_double),
        ("mLateralGroundVel", C.c_double),
        ("mLongitudinalGroundVel", C.c_double),
        ("mCamber", C.c_double),
        ("mLateralForce", C.c_double),
        ("mLongitudinalForce", C.c_double),
        ("mTireLoad", C.c_double),
        ("mGripFract", C.c_double),
        ("mPressure", C.c_double),
        ("mTemperature", C.c_double * 3),
        ("mWear", C.c_double),
        ("mTerrainName", C.c_char * 16),
        ("mSurfaceType", C.c_ubyte),
        ("mFlat", C.c_bool),
        ("mDetached", C.c_bool),
        ("mStaticUndeflectedRadius", C.c_ubyte),
        ("mVerticalTireDeflection", C.c_double),
        ("mWheelYLocation", C.c_double),
        ("mToe", C.c_double),
        ("mTireCarcassTemperature", C.c_double),
        ("mTireInnerLayerTemperature", C.c_double * 3),
        ("mExpansion", C.c_ubyte * 24),
    ]


class VehicleTelemetry(_Packed):
    _fields_ = [
        ("mID", C.c_int32),
        ("mDeltaTime", C.c_double),
        ("mElapsedTime", C.c_double),
        ("mLapNumber", C.c_int32),
        ("mLapStartET", C.c_double),
        ("mVehicleName", C.c_char * 64),
        ("mTrackName", C.c_char * 64),
        ("mPos", Vec3),
        ("mLocalVel", Vec3),
        ("mLocalAccel", Vec3),
        ("mOri", Vec3 * 3),
        ("mLocalRot", Vec3),
        ("mLocalRotAccel", Vec3),
        ("mGear", C.c_int32),
        ("mEngineRPM", C.c_double),
        ("mEngineWaterTemp", C.c_double),
        ("mEngineOilTemp", C.c_double),
        ("mClutchRPM", C.c_double),
        ("mUnfilteredThrottle", C.c_double),
        ("mUnfilteredBrake", C.c_double),
        ("mUnfilteredSteering", C.c_double),
        ("mUnfilteredClutch", C.c_double),
        ("mFilteredThrottle", C.c_double),
        ("mFilteredBrake", C.c_double),
        ("mFilteredSteering", C.c_double),
        ("mFilteredClutch", C.c_double),
        ("mSteeringShaftTorque", C.c_double),
        ("mFront3rdDeflection", C.c_double),
        ("mRear3rdDeflection", C.c_double),
        ("mFrontWingHeight", C.c_double),
        ("mFrontRideHeight", C.c_double),
        ("mRearRideHeight", C.c_double),
        ("mDrag", C.c_double),
        ("mFrontDownforce", C.c_double),
        ("mRearDownforce", C.c_double),
        ("mFuel", C.c_double),
        ("mEngineMaxRPM", C.c_double),
        ("mScheduledStops", C.c_ubyte),
        ("mOverheating", C.c_bool),
        ("mDetached", C.c_bool),
        ("mHeadlights", C.c_bool),
        ("mDentSeverity", C.c_ubyte * 8),
        ("mLastImpactET", C.c_double),
        ("mLastImpactMagnitude", C.c_double),
        ("mLastImpactPos", Vec3),
        ("mEngineTorque", C.c_double),
        ("mCurrentSector", C.c_int32),
        ("mSpeedLimiter", C.c_ubyte),
        ("mMaxGears", C.c_ubyte),
        ("mFrontTireCompoundIndex", C.c_ubyte),
        ("mRearTireCompoundIndex", C.c_ubyte),
        ("mFuelCapacity", C.c_double),
        ("mFrontFlapActivated", C.c_ubyte),
        ("mRearFlapActivated", C.c_ubyte),
        ("mRearFlapLegalStatus", C.c_ubyte),
        ("mIgnitionStarter", C.c_ubyte),
        ("mFrontTireCompoundName", C.c_char * 18),
        ("mRearTireCompoundName", C.c_char * 18),
        ("mSpeedLimiterAvailable", C.c_ubyte),
        ("mAntiStallActivated", C.c_ubyte),
        ("mUnused", C.c_ubyte * 2),
        ("mVisualSteeringWheelRange", C.c_float),
        ("mRearBrakeBias", C.c_double),
        ("mTurboBoostPressure", C.c_double),
        ("mPhysicsToGraphicsOffset", C.c_float * 3),
        ("mPhysicalSteeringWheelRange", C.c_float),
        ("mBatteryChargeFraction", C.c_double),
        ("mElectricBoostMotorTorque", C.c_double),
        ("mElectricBoostMotorRPM", C.c_double),
        ("mElectricBoostMotorTemperature", C.c_double),
        ("mElectricBoostWaterTemperature", C.c_double),
        ("mElectricBoostMotorState", C.c_ubyte),
        ("mExpansion", C.c_ubyte * 111),
        ("mWheels", Wheel * 4),
    ]


class ScoringInfo(_Packed):
    _fields_ = [
        ("mTrackName", C.c_char * 64),
        ("mSession", C.c_int32),
        ("mCurrentET", C.c_double),
        ("mEndET", C.c_double),
        ("mMaxLaps", C.c_int32),
        ("mLapDist", C.c_double),
        ("pointer1", C.c_ubyte * 8),
        ("mNumVehicles", C.c_int32),
        ("mGamePhase", C.c_ubyte),
        ("mYellowFlagState", C.c_byte),
        ("mSectorFlag", C.c_byte * 3),
        ("mStartLight", C.c_ubyte),
        ("mNumRedLights", C.c_ubyte),
        ("mInRealtime", C.c_bool),
        ("mPlayerName", C.c_char * 32),
        ("mPlrFileName", C.c_char * 64),
        ("mDarkCloud", C.c_double),
        ("mRaining", C.c_double),
        ("mAmbientTemp", C.c_double),
        ("mTrackTemp", C.c_double),
        ("mWind", Vec3),
        ("mMinPathWetness", C.c_double),
        ("mMaxPathWetness", C.c_double),
        ("mGameMode", C.c_ubyte),
        ("mIsPasswordProtected", C.c_bool),
        ("mServerPort", C.c_uint16),
        ("mServerPublicIP", C.c_uint32),
        ("mMaxPlayers", C.c_int32),
        ("mServerName", C.c_char * 32),
        ("mStartET", C.c_float),
        ("mAvgPathWetness", C.c_double),
        ("mExpansion", C.c_ubyte * 200),
        ("pointer2", C.c_ubyte * 8),
    ]


class VehicleScoring(_Packed):
    _fields_ = [
        ("mID", C.c_int32),
        ("mDriverName", C.c_char * 32),
        ("mVehicleName", C.c_char * 64),
        ("mTotalLaps", C.c_int16),
        ("mSector", C.c_byte),
        ("mFinishStatus", C.c_byte),
        ("mLapDist", C.c_double),
        ("mPathLateral", C.c_double),
        ("mTrackEdge", C.c_double),
        ("mBestSector1", C.c_double),
        ("mBestSector2", C.c_double),
        ("mBestLapTime", C.c_double),
        ("mLastSector1", C.c_double),
        ("mLastSector2", C.c_double),
        ("mLastLapTime", C.c_double),
        ("mCurSector1", C.c_double),
        ("mCurSector2", C.c_double),
        ("mNumPitstops", C.c_int16),
        ("mNumPenalties", C.c_int16),
        ("mIsPlayer", C.c_bool),
        ("mControl", C.c_byte),
        ("mInPits", C.c_bool),
        ("mPlace", C.c_ubyte),
        ("mVehicleClass", C.c_char * 32),
        ("mTimeBehindNext", C.c_double),
        ("mLapsBehindNext", C.c_int32),
        ("mTimeBehindLeader", C.c_double),
        ("mLapsBehindLeader", C.c_int32),
        ("mLapStartET", C.c_double),
        ("mPos", Vec3),
        ("mLocalVel", Vec3),
        ("mLocalAccel", Vec3),
        ("mOri", Vec3 * 3),
        ("mLocalRot", Vec3),
        ("mLocalRotAccel", Vec3),
        ("mHeadlights", C.c_ubyte),
        ("mPitState", C.c_ubyte),
        ("mServerScored", C.c_ubyte),
        ("mIndividualPhase", C.c_ubyte),
        ("mQualification", C.c_int32),
        ("mTimeIntoLap", C.c_double),
        ("mEstimatedLapTime", C.c_double),
        ("mPitGroup", C.c_char * 24),
        ("mFlag", C.c_ubyte),
        ("mUnderYellow", C.c_bool),
        ("mCountLapFlag", C.c_ubyte),
        ("mInGarageStall", C.c_bool),
        ("mUpgradePack", C.c_ubyte * 16),
        ("mPitLapDist", C.c_float),
        ("mBestLapSector1", C.c_float),
        ("mBestLapSector2", C.c_float),
        ("mExpansion", C.c_ubyte * 48),
    ]


class TelemetryBuffer(_Packed):
    """Version block plus rF2Telemetry as laid out in the mapped file."""

    _fields_ = [
        ("mVersionUpdateBegin", C.c_uint32),
        ("mVersionUpdateEnd", C.c_uint32),
        ("mBytesUpdatedHint", C.c_int32),
        ("mNumVehicles", C.c_int32),
        ("mVehicles", VehicleTelemetry * MAX_MAPPED_VEHICLES),
    ]


class ScoringBuffer(_Packed):
    """Version block plus rF2Scoring as laid out in the mapped file."""

    _fields_ = [
        ("mVersionUpdateBegin", C.c_uint32),
        ("mVersionUpdateEnd", C.c_uint32),
        ("mBytesUpdatedHint", C.c_int32),
        ("mScoringInfo", ScoringInfo),
        ("mVehicles", VehicleScoring * MAX_MAPPED_VEHICLES),
    ]


class PitMenu(_Packed):
    """The row of the pit menu that is selected in the game."""

    _fields_ = [
        ("mCategoryIndex", C.c_int32),
        ("mCategoryName", C.c_char * 32),
        ("mChoiceIndex", C.c_int32),
        ("mChoiceString", C.c_char * 32),
        ("mNumChoices", C.c_int32),
        ("mExpansion", C.c_ubyte * 256),
    ]


class PitInfoBuffer(_Packed):
    """Version block plus rF2PitInfo as laid out in the mapped file (this buffer has no bytes-updated hint)."""

    _fields_ = [
        ("mVersionUpdateBegin", C.c_uint32),
        ("mVersionUpdateEnd", C.c_uint32),
        ("mPitMenu", PitMenu),
    ]


HW_CONTROL_FILE = "$rFactor2SMMP_HWControl$"


class HWControlBuffer(_Packed):
    """Version block plus rF2HWControl: the plugin hands the named control to the game as if it was pressed."""

    _fields_ = [
        ("mVersionUpdateBegin", C.c_uint32),
        ("mVersionUpdateEnd", C.c_uint32),
        ("mLayoutVersion", C.c_int32),
        ("mControlName", C.c_char * 96),
        ("mfRetVal", C.c_double),
    ]
