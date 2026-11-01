package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func TestCommandReachesTheAgentAndItsResultIsRecorded(t *testing.T) {
	srv, _, url := setup(t, "")
	srv.Cmds = commands.New()
	steps := []wire.Step{{Category: "FUEL:", Choice: "max"}}
	if _, err := srv.Cmds.Send(context.Background(), 0, "Max", steps); !errors.Is(err, commands.ErrNotConnected) {
		t.Fatalf("err=%v", err)
	}
	c := dial(t, url, "")
	send(t, c, "a", 0, "hello", `{"agent_version":"1"}`)

	id, err := srv.Cmds.Send(context.Background(), 0, "Max", steps)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cmd wire.Command
	if err := json.Unmarshal(data, &cmd); err != nil || cmd.ID != id || cmd.Type != "command" || len(cmd.Steps) != 1 {
		t.Fatalf("cmd=%+v err=%v", cmd, err)
	}
	if st := srv.Cmds.Status(0); st.State != commands.Pending {
		t.Fatalf("%+v", st)
	}
	if _, err := srv.Cmds.Send(context.Background(), 0, "again", steps); !errors.Is(err, commands.ErrBusy) {
		t.Fatalf("err=%v", err)
	}

	send(t, c, "a", 0, "command_result", `{"id":"`+id+`","ok":true,"message":"ok","applied":[{"category":"FUEL:","choice":"+122/6"}]}`)
	st := srv.Cmds.Status(0)
	if st.State != commands.Done || len(st.Applied) != 1 || st.Applied[0].Choice != "+122/6" {
		t.Fatalf("%+v", st)
	}
	// an answer to an unknown command changes nothing
	send(t, c, "a", 0, "command_result", `{"id":"other","ok":false,"message":"x"}`)
	if st := srv.Cmds.Status(0); st.State != commands.Done {
		t.Fatalf("%+v", st)
	}
}

func TestPitRequestIsSentAsItsOwnKind(t *testing.T) {
	srv, _, url := setup(t, "")
	srv.Cmds = commands.New()
	c := dial(t, url, "")
	send(t, c, "a", 0, "hello", `{"agent_version":"1"}`)
	id, err := srv.Cmds.PitRequest(context.Background(), 0, "Stopp")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cmd wire.Command
	if err := json.Unmarshal(data, &cmd); err != nil || cmd.ID != id || cmd.Kind != wire.CommandPitRequest || len(cmd.Steps) != 0 {
		t.Fatalf("cmd=%+v err=%v", cmd, err)
	}
}

func TestAgentsAreListedWithVersionFreshnessAndDisconnect(t *testing.T) {
	srv, _, url := setup(t, "")
	srv.Cmds = commands.New()
	c := dial(t, url, "")
	send(t, c, "driver-pc", 0, "hello", `{"agent_version":"0.3.1"}`)
	list := srv.Cmds.Agents()
	if len(list) != 1 || list[0].AgentID != "driver-pc" || list[0].Role != "driver" || list[0].Version != "0.3.1" ||
		!list[0].Connected || list[0].SessionID != "s1" || list[0].LastData.IsZero() {
		t.Fatalf("%+v", list)
	}
	c.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && srv.Cmds.Agents()[0].Connected {
		time.Sleep(20 * time.Millisecond)
	}
	if got := srv.Cmds.Agents(); len(got) != 1 || got[0].Connected || got[0].Version != "0.3.1" {
		t.Fatalf("after disconnect: %+v", got)
	}
}

// Two agents of one team: a command goes to the agent it is for, and one agent's pending command does not block the other.
func TestCommandsGoToTheAgentTheyAreFor(t *testing.T) {
	ctx := context.Background()
	db := store.NewMemory()
	one, _ := db.CreateAgent(ctx, "one", 1, store.HashToken("token-one"))
	two, _ := db.CreateAgent(ctx, "two", 1, store.HashToken("token-two"))
	srv := New(db.ForTeam(1), &live.State{}, nil)
	srv.Cmds = commands.New()
	ts := httptest.NewServer(&Gateway{Dir: db, DefaultTeam: 1, Server: func(int64) *Server { return srv }})
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	// both call themselves the same; the registered agent tells them apart
	c1, c2 := dial(t, url, "token-one"), dial(t, url, "token-two")
	send(t, c1, "pc", 0, "hello", `{"agent_version":"1"}`)
	send(t, c2, "pc", 0, "hello", `{"agent_version":"1"}`)
	if got := srv.Cmds.Agents(); len(got) != 2 || got[0].TokenID != one.ID || got[1].TokenID != two.ID {
		t.Fatalf("agents %+v", got)
	}

	read := func(c *websocket.Conn) wire.Command {
		t.Helper()
		rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, data, err := c.Read(rctx)
		if err != nil {
			t.Fatal(err)
		}
		var cmd wire.Command
		if err := json.Unmarshal(data, &cmd); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	id2, err := srv.Cmds.PitRequest(ctx, two.ID, "box")
	if err != nil {
		t.Fatal(err)
	}
	if cmd := read(c2); cmd.ID != id2 {
		t.Fatalf("agent two got %+v", cmd)
	}
	if _, err := srv.Cmds.PitRequest(ctx, two.ID, "again"); !errors.Is(err, commands.ErrBusy) {
		t.Fatalf("second command for agent two: %v", err)
	}
	id1, err := srv.Cmds.PitRequest(ctx, one.ID, "box")
	if err != nil {
		t.Fatalf("agent one is blocked by agent two: %v", err)
	}
	if cmd := read(c1); cmd.ID != id1 {
		t.Fatalf("agent one got %+v", cmd)
	}
	if srv.Cmds.Status(one.ID).ID != id1 || srv.Cmds.Status(two.ID).ID != id2 || srv.Cmds.Status(0) != nil {
		t.Fatal("the status of a command is kept per agent")
	}
	// the answer of agent two ends only its own command
	send(t, c2, "pc", 0, "command_result", `{"id":"`+id2+`","ok":true,"message":"ok"}`)
	if srv.Cmds.Status(two.ID).State != commands.Done || srv.Cmds.Status(one.ID).State != commands.Pending {
		t.Fatalf("one %+v two %+v", srv.Cmds.Status(one.ID), srv.Cmds.Status(two.ID))
	}
	if _, err := srv.Cmds.PitRequest(ctx, 99, "box"); !errors.Is(err, commands.ErrNotConnected) {
		t.Fatalf("agent that is not connected: %v", err)
	}
}

func TestOutdatedVersions(t *testing.T) {
	for _, c := range []struct {
		version, min string
		want         bool
	}{
		{"0.1.0", "0.2.0", true}, {"0.2.0", "0.2.0", false}, {"0.10.0", "0.9.0", false}, {"0.9", "0.10.0", true},
		{"v1.0.0", "1.0.1", true}, {"1.0.0", "1.0", false},
		{"", "0.2.0", false}, {"0.1.0", "", false}, {"dev", "0.2.0", false}, {"0.1.0", "x", false},
	} {
		if got := commands.Outdated(c.version, c.min); got != c.want {
			t.Errorf("Outdated(%q, %q) = %v, want %v", c.version, c.min, got, c.want)
		}
	}
}

func TestTheAgentListMarksOutdatedAgents(t *testing.T) {
	srv, _, url := setup(t, "")
	srv.Cmds = commands.New()
	srv.Cmds.MinVersion = "0.3.0"
	send(t, dial(t, url, ""), "old-pc", 0, "hello", `{"agent_version":"0.2.5"}`)
	if got := srv.Cmds.Agents(); len(got) != 1 || !got[0].Outdated {
		t.Fatalf("%+v", got)
	}
	srv.Cmds.MinVersion = ""
	if got := srv.Cmds.Agents(); got[0].Outdated {
		t.Fatalf("without a minimum nothing is outdated: %+v", got)
	}
}
