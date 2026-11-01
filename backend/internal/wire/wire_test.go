package wire

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestDecodeSampleRecording(t *testing.T) {
	f, err := os.Open("../../../testdata/sample.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var types []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m, err := Decode(sc.Bytes())
		if err != nil {
			t.Fatalf("%s: %v", sc.Text(), err)
		}
		types = append(types, m.Type)
	}
	if strings.Join(types, ",") != "hello,telemetry,scoring,lap" {
		t.Fatalf("types=%v", types)
	}
}

const env = `{"v":1,"type":"%s","agent_id":"a","role":"driver","session_id":"s","seq":1,"et":1,"payload":%s}`

func msg(typ, payload string) []byte {
	return []byte(strings.Replace(strings.Replace(env, "%s", typ, 1), "%s", payload, 1))
}

func TestDecodeRejects(t *testing.T) {
	cases := map[string][]byte{
		"not json":          []byte("nope"),
		"wrong version":     []byte(`{"v":2,"type":"hello","agent_id":"a","role":"driver","session_id":"s","seq":0,"et":0,"payload":{"agent_version":"1"}}`),
		"bad role":          []byte(`{"v":1,"type":"hello","agent_id":"a","role":"x","session_id":"s","seq":0,"et":0,"payload":{"agent_version":"1"}}`),
		"unknown type":      msg("bogus", `{}`),
		"unknown field":     msg("hello", `{"agent_version":"1","extra":1}`),
		"lap no vehicle":    msg("lap", `{"car_id":0,"lap":1,"in_lap":false,"out_lap":false}`),
		"lap zero":          msg("lap", `{"car_id":0,"vehicle_name":"v","lap":0}`),
		"telemetry 3 tyres": msg("telemetry", `{"car_id":0,"vehicle_name":"v","lap":1,"fuel":1,"fuel_capacity":90,"wear":[1,1,1]}`),
		"event bad kind":    msg("event", `{"kind":"x","car_id":0,"vehicle_name":"v"}`),
		"no payload":        []byte(`{"v":1,"type":"hello","agent_id":"a","role":"driver","session_id":"s","seq":0,"et":0}`),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// Agents of version 0.1 send a role and newer ones do not; both are fine, the role is ignored.
func TestDecodeRoleIsOptional(t *testing.T) {
	for name, data := range map[string]string{
		"driver":  `{"v":1,"type":"hello","agent_id":"a","role":"driver","session_id":"s","seq":0,"et":0,"payload":{"agent_version":"1"}}`,
		"spotter": `{"v":1,"type":"hello","agent_id":"a","role":"spotter","session_id":"s","seq":0,"et":0,"payload":{"agent_version":"1"}}`,
		"none":    `{"v":1,"type":"hello","agent_id":"a","session_id":"s","seq":0,"et":0,"payload":{"agent_version":"1"}}`,
	} {
		if _, err := Decode([]byte(data)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDecodeScoring(t *testing.T) {
	m, err := Decode(msg("scoring", `{"track":"t","session_type":"race","end_et":86400,"vehicles":[{"car_id":1,"vehicle_name":"v","driver":"d","vehicle_class":"c","place":1,"laps":3,"last_lap":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	s := m.Body.(Scoring)
	if len(s.Vehicles) != 1 || s.Vehicles[0].LastLap != nil {
		t.Fatalf("%+v", s)
	}
}

func TestScoringVehicleSectorFieldsAreOptional(t *testing.T) {
	msg := func(vehicle string) string {
		return `{"v":1,"type":"scoring","agent_id":"a","role":"driver","session_id":"s","seq":0,"et":1,"payload":{"track":"t","session_type":"race","end_et":10,"sector_flags":[1,11,11],"vehicles":[` + vehicle + `]}}`
	}
	base := `"car_id":0,"vehicle_name":"v","driver":"d","vehicle_class":"c","place":1,"laps":1`
	if _, err := Decode([]byte(msg("{" + base + "}"))); err != nil {
		t.Fatalf("a vehicle from an older agent must decode: %v", err)
	}
	m, err := Decode([]byte(msg("{" + base + `,"sector":2,"flag":1,"under_yellow":true}`)))
	if err != nil {
		t.Fatal(err)
	}
	v := m.Body.(Scoring).Vehicles[0]
	if v.Sector == nil || *v.Sector != 2 || v.Flag == nil || *v.Flag != 1 || !v.UnderYellow {
		t.Fatalf("%+v", v)
	}
	if _, err := Decode([]byte(msg("{" + base + `,"sector":4}`))); err == nil {
		t.Fatal("sector 4 must be refused")
	}
}
