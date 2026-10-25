// Package wire decodes and validates the agent messages defined in schema/messages.schema.json.
package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Version is the supported wire format version.
const Version = 1

// Message types.
const (
	TypeHello     = "hello"
	TypeScoring   = "scoring"
	TypeTelemetry = "telemetry"
	TypeLap       = "lap"
	TypeEvent     = "event"
)

// Envelope is the common part of every agent message.
type Envelope struct {
	V         int             `json:"v"`
	Type      string          `json:"type"`
	AgentID   string          `json:"agent_id"`
	Role      string          `json:"role"`
	SessionID string          `json:"session_id"`
	Seq       int64           `json:"seq"`
	ET        float64         `json:"et"`
	Payload   json.RawMessage `json:"payload"`
}

// Message is a decoded agent message. Body is one of Hello, Scoring, Telemetry, Lap, Event.
type Message struct {
	Envelope
	Body any
}

// Ack is the only backend to agent message.
type Ack struct {
	Type string `json:"type"`
	Seq  int64  `json:"seq"`
}

// NewAck returns an acknowledgement for seq.
func NewAck(seq int64) Ack { return Ack{Type: "ack", Seq: seq} }

type Hello struct {
	AgentVersion  string `json:"agent_version"`
	ResumeFromSeq *int64 `json:"resume_from_seq"`
}

type Weather struct {
	AmbientTemp *float64 `json:"ambient_temp"`
	TrackTemp   *float64 `json:"track_temp"`
	Rain        *float64 `json:"rain"`
}

type ScoringVehicle struct {
	CarID            int      `json:"car_id"`
	VehicleName      string   `json:"vehicle_name"`
	Driver           string   `json:"driver"`
	VehicleClass     string   `json:"vehicle_class"`
	IsPlayer         bool     `json:"is_player"`
	Control          *int     `json:"control"`
	Place            int      `json:"place"`
	Laps             int      `json:"laps"`
	LastLap          *float64 `json:"last_lap"`
	BestLap          *float64 `json:"best_lap"`
	LastS1           *float64 `json:"last_s1"`
	LastS2           *float64 `json:"last_s2"`
	InPits           bool     `json:"in_pits"`
	PitState         int      `json:"pit_state"`
	NumPitstops      int      `json:"num_pitstops"`
	TimeBehindNext   *float64 `json:"time_behind_next"`
	LapsBehindLeader int      `json:"laps_behind_leader"`
}

type Scoring struct {
	Track       string           `json:"track"`
	SessionType string           `json:"session_type"`
	EndET       float64          `json:"end_et"`
	GamePhase   *int             `json:"game_phase"`
	YellowState *int             `json:"yellow_state"`
	SectorFlags []int            `json:"sector_flags"`
	Weather     *Weather         `json:"weather"`
	Vehicles    []ScoringVehicle `json:"vehicles"`
}

type Telemetry struct {
	CarID           int       `json:"car_id"`
	VehicleName     string    `json:"vehicle_name"`
	Lap             int       `json:"lap"`
	LapDist         *float64  `json:"lap_dist"`
	Fuel            float64   `json:"fuel"`
	FuelCapacity    float64   `json:"fuel_capacity"`
	Wear            []float64 `json:"wear"`
	TyreCarcassTemp []float64 `json:"tyre_carcass_temp"`
	Damage          *float64  `json:"damage"`
}

type Lap struct {
	CarID       int       `json:"car_id"`
	VehicleName string    `json:"vehicle_name"`
	Lap         int       `json:"lap"`
	Driver      string    `json:"driver"`
	LapTime     *float64  `json:"lap_time"`
	S1          *float64  `json:"s1"`
	S2          *float64  `json:"s2"`
	S3          *float64  `json:"s3"`
	FuelStart   *float64  `json:"fuel_start"`
	FuelEnd     *float64  `json:"fuel_end"`
	TyreAge     *int      `json:"tyre_age"`
	Wear        []float64 `json:"wear"`
	FCY         bool      `json:"fcy"`
	InLap       bool      `json:"in_lap"`
	OutLap      bool      `json:"out_lap"`
}

// Event kinds.
const (
	EventPitEntry   = "pit_entry"
	EventPitExit    = "pit_exit"
	EventDriverSwap = "driver_swap"
	EventFCYStart   = "fcy_start"
	EventFCYEnd     = "fcy_end"
)

type Event struct {
	Kind         string   `json:"kind"`
	CarID        int      `json:"car_id"`
	VehicleName  string   `json:"vehicle_name"`
	Driver       string   `json:"driver"`
	FuelAdded    *float64 `json:"fuel_added"`
	TyresChanged *bool    `json:"tyres_changed"`
}

// Decode parses and validates one message.
func Decode(data []byte) (Message, error) {
	var env Envelope
	if err := strictUnmarshal(data, &env); err != nil {
		return Message{}, fmt.Errorf("envelope: %w", err)
	}
	if env.V != Version {
		return Message{}, fmt.Errorf("unsupported wire version %d", env.V)
	}
	if env.AgentID == "" || env.SessionID == "" {
		return Message{}, errors.New("agent_id and session_id are required")
	}
	if env.Role != "driver" && env.Role != "spotter" {
		return Message{}, fmt.Errorf("invalid role %q", env.Role)
	}
	if env.Seq < 0 || env.ET < 0 {
		return Message{}, errors.New("seq and et must not be negative")
	}
	if len(env.Payload) == 0 {
		return Message{}, errors.New("payload is required")
	}
	msg := Message{Envelope: env}
	var err error
	switch env.Type {
	case TypeHello:
		var b Hello
		if err = strictUnmarshal(env.Payload, &b); err == nil && b.AgentVersion == "" {
			err = errors.New("agent_version is required")
		}
		msg.Body = b
	case TypeScoring:
		var b Scoring
		if err = strictUnmarshal(env.Payload, &b); err == nil {
			err = b.validate()
		}
		msg.Body = b
	case TypeTelemetry:
		var b Telemetry
		if err = strictUnmarshal(env.Payload, &b); err == nil {
			err = b.validate()
		}
		msg.Body = b
	case TypeLap:
		var b Lap
		if err = strictUnmarshal(env.Payload, &b); err == nil {
			err = b.validate()
		}
		msg.Body = b
	case TypeEvent:
		var b Event
		if err = strictUnmarshal(env.Payload, &b); err == nil {
			err = b.validate()
		}
		msg.Body = b
	default:
		err = fmt.Errorf("unknown type %q", env.Type)
	}
	if err != nil {
		return Message{}, fmt.Errorf("%s payload: %w", env.Type, err)
	}
	return msg, nil
}

func strictUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s Scoring) validate() error {
	if s.Track == "" || s.SessionType == "" {
		return errors.New("track and session_type are required")
	}
	if len(s.SectorFlags) != 0 && len(s.SectorFlags) != 3 {
		return errors.New("sector_flags needs 3 values")
	}
	for _, v := range s.Vehicles {
		if v.VehicleName == "" || v.Place < 1 {
			return fmt.Errorf("vehicle %d: vehicle_name and place >= 1 required", v.CarID)
		}
	}
	return nil
}

func (t Telemetry) validate() error {
	if t.VehicleName == "" {
		return errors.New("vehicle_name is required")
	}
	if t.Fuel < 0 || t.FuelCapacity <= 0 {
		return errors.New("fuel must be >= 0 and fuel_capacity > 0")
	}
	if len(t.Wear) != 4 {
		return errors.New("wear needs 4 values")
	}
	if len(t.TyreCarcassTemp) != 0 && len(t.TyreCarcassTemp) != 4 {
		return errors.New("tyre_carcass_temp needs 4 values")
	}
	return nil
}

func (l Lap) validate() error {
	if l.VehicleName == "" || l.Lap < 1 {
		return errors.New("vehicle_name and lap >= 1 required")
	}
	if len(l.Wear) != 0 && len(l.Wear) != 4 {
		return errors.New("wear needs 4 values")
	}
	return nil
}

func (e Event) validate() error {
	switch e.Kind {
	case EventPitEntry, EventPitExit, EventDriverSwap, EventFCYStart, EventFCYEnd:
	default:
		return fmt.Errorf("unknown kind %q", e.Kind)
	}
	if e.VehicleName == "" {
		return errors.New("vehicle_name is required")
	}
	return nil
}
