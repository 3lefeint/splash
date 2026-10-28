package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func TestCommandReachesTheDriverAgentAndItsResultIsRecorded(t *testing.T) {
	srv, _, url := setup(t, "")
	srv.Cmds = commands.New()
	steps := []wire.Step{{Category: "FUEL:", Choice: "max"}}
	if _, err := srv.Cmds.Send(context.Background(), "Max", steps); !errors.Is(err, commands.ErrNoDriver) {
		t.Fatalf("err=%v", err)
	}
	c := dial(t, url, "")
	send(t, c, "a", 0, "hello", `{"agent_version":"1"}`)

	id, err := srv.Cmds.Send(context.Background(), "Max", steps)
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
	if st := srv.Cmds.Status(); st.State != commands.Pending {
		t.Fatalf("%+v", st)
	}
	if _, err := srv.Cmds.Send(context.Background(), "again", steps); !errors.Is(err, commands.ErrBusy) {
		t.Fatalf("err=%v", err)
	}

	send(t, c, "a", 0, "command_result", `{"id":"`+id+`","ok":true,"message":"ok","applied":[{"category":"FUEL:","choice":"+122/6"}]}`)
	st := srv.Cmds.Status()
	if st.State != commands.Done || len(st.Applied) != 1 || st.Applied[0].Choice != "+122/6" {
		t.Fatalf("%+v", st)
	}
	// an answer to an unknown command changes nothing
	send(t, c, "a", 0, "command_result", `{"id":"other","ok":false,"message":"x"}`)
	if st := srv.Cmds.Status(); st.State != commands.Done {
		t.Fatalf("%+v", st)
	}
}

func TestPitRequestIsSentAsItsOwnKind(t *testing.T) {
	srv, _, url := setup(t, "")
	srv.Cmds = commands.New()
	c := dial(t, url, "")
	send(t, c, "a", 0, "hello", `{"agent_version":"1"}`)
	id, err := srv.Cmds.PitRequest(context.Background(), "Stopp")
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
