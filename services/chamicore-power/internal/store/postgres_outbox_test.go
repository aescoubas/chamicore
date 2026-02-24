//go:build integration

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	natssrv "github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"git.cscs.ch/openchami/chamicore-lib/events"
	eventsnats "git.cscs.ch/openchami/chamicore-lib/events/nats"
	"git.cscs.ch/openchami/chamicore-lib/events/outbox"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/testutil"
	"git.cscs.ch/openchami/chamicore-power/internal/engine"
	"git.cscs.ch/openchami/chamicore-power/internal/model"
	powersmd "git.cscs.ch/openchami/chamicore-power/internal/smd"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

func TestRunner_TransitionPatchesSMDAndPublishesOutboxEvents(t *testing.T) {
	db := testutil.NewTestPostgres(t, "../../migrations/postgres")
	st := NewPostgresStore(db)

	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
CREATE OR REPLACE VIEW outbox AS
SELECT id, event_type, subject, data, created_at, sent_at
FROM power.outbox
`)
	require.NoError(t, err)

	_, err = st.ReplaceTopologyMappings(ctx, []model.BMCEndpoint{
		{
			BMCID:        "bmc-1",
			Endpoint:     "https://bmc-1",
			CredentialID: "cred-1",
			Source:       "smd",
		},
	}, []model.NodeBMCLink{
		{
			NodeID: "node-1",
			BMCID:  "bmc-1",
			Source: "smd",
		},
	}, time.Now().UTC())
	require.NoError(t, err)

	patchStateCh := make(chan string, 1)
	smdServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/hsm/v2/State/Components/node-1" {
			http.NotFound(w, r)
			return
		}

		var req smdtypes.PatchComponentRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotNil(t, req.State)

		select {
		case patchStateCh <- strings.TrimSpace(*req.State):
		default:
		}

		now := time.Now().UTC()
		_ = json.NewEncoder(w).Encode(httputil.Resource[smdtypes.Component]{
			Kind:       "Component",
			APIVersion: "hsm/v2",
			Metadata: httputil.Metadata{
				ID:        "node-1",
				CreatedAt: now,
				UpdatedAt: now,
			},
			Spec: smdtypes.Component{
				ID:    "node-1",
				Type:  "Node",
				State: *req.State,
			},
		})
	}))
	defer smdServer.Close()

	smd := smdclient.New(smdclient.Config{
		BaseURL: smdServer.URL,
	})
	updater := powersmd.NewUpdater(smd)

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	runner := engine.New(st, engine.NoopExecutor{}, engine.ExpectedStateReader{}, engine.Config{
		RetryAttempts:      1,
		VerificationWindow: 2 * time.Second,
		VerificationPoll:   10 * time.Millisecond,
		TransitionDeadline: 2 * time.Second,
	}, engine.WithNodeStateUpdater(updater))
	runner.Start(runCtx)

	transition, err := runner.StartTransition(context.Background(), engine.StartRequest{
		RequestID:   "req-p8.8",
		RequestedBy: "integration-test",
		Operation:   "On",
		NodeIDs:     []string{"node-1"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, transition.ID)

	finalTransition := waitForTransitionTerminal(t, st, transition.ID)
	assert.Equal(t, engine.TransitionStateCompleted, finalTransition.State)
	assert.Equal(t, 1, finalTransition.SuccessCount)
	assert.Equal(t, 0, finalTransition.FailureCount)

	select {
	case patchedState := <-patchStateCh:
		assert.Equal(t, "Ready", patchedState)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SMD patch request")
	}

	tasks, err := st.ListTransitionTasks(ctx, transition.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, engine.TaskStateSucceeded, tasks[0].State)
	assert.Equal(t, "On", tasks[0].FinalPowerState)

	unsentCountBeforeRelay := countUnsentOutboxRows(t, db)
	assert.GreaterOrEqual(t, unsentCountBeforeRelay, 3)

	natsURL := startEmbeddedNATS(t)
	publisher, err := eventsnats.NewPublisher(eventsnats.Config{
		URL:  natsURL,
		Name: "chamicore-power-outbox-integration",
		Stream: eventsnats.StreamConfig{
			Name:     "CHAMICORE_POWER",
			Subjects: []string{"chamicore.power.>"},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, publisher.Close())
	})

	conn, err := natsgo.Connect(natsURL)
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	js, err := conn.JetStream()
	require.NoError(t, err)

	relay, err := outbox.NewRelay(db, publisher, outbox.Config{
		PollInterval:         10 * time.Millisecond,
		RetryInitialInterval: 10 * time.Millisecond,
		RetryMaxInterval:     50 * time.Millisecond,
	})
	require.NoError(t, err)

	relayCtx, relayCancel := context.WithCancel(context.Background())
	relayDone := make(chan error, 1)
	go func() {
		relayDone <- relay.Run(relayCtx)
	}()
	t.Cleanup(func() {
		relayCancel()
		select {
		case runErr := <-relayDone:
			require.NoError(t, runErr)
		case <-time.After(5 * time.Second):
			t.Fatal("relay did not stop in time")
		}
	})

	require.Eventually(t, func() bool {
		return countUnsentOutboxRows(t, db) == 0
	}, 10*time.Second, 50*time.Millisecond)

	publishedEvents := streamEvents(t, js, "CHAMICORE_POWER")
	lifecycleEvent, found := findEvent(publishedEvents, func(event events.Event) bool {
		if event.Type != transitionLifecycleEventType || event.Subject != transition.ID {
			return false
		}
		var payload transitionLifecycleEventData
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return false
		}
		return payload.Snapshot.State == engine.TransitionStateCompleted
	})
	require.True(t, found, "expected transition lifecycle completed event")
	assert.Equal(t, transitionEventSource, lifecycleEvent.Source)

	taskResultEvent, found := findEvent(publishedEvents, func(event events.Event) bool {
		if event.Type != transitionTaskResultEventType || event.Subject != "node-1" {
			return false
		}
		var payload transitionTaskResultEventData
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return false
		}
		return payload.Snapshot.State == engine.TaskStateSucceeded &&
			payload.TransitionID == transition.ID
	})
	require.True(t, found, "expected transition task result event")
	assert.Equal(t, transitionEventSource, taskResultEvent.Source)
}

func waitForTransitionTerminal(t *testing.T, st *PostgresStore, transitionID string) engine.Transition {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		transition, err := st.GetTransition(context.Background(), transitionID)
		require.NoError(t, err)
		switch transition.State {
		case engine.TransitionStateCompleted,
			engine.TransitionStateFailed,
			engine.TransitionStatePartial,
			engine.TransitionStateCanceled,
			engine.TransitionStatePlanned:
			return transition
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for terminal transition state for %q", transitionID)
	return engine.Transition{}
}

func countUnsentOutboxRows(t *testing.T, db *sql.DB) int {
	t.Helper()

	var count int
	err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM power.outbox WHERE sent_at IS NULL").Scan(&count)
	require.NoError(t, err)
	return count
}

func streamEvents(t *testing.T, js natsgo.JetStreamContext, stream string) []events.Event {
	t.Helper()

	info, err := js.StreamInfo(stream)
	require.NoError(t, err)
	require.NotNil(t, info)

	items := make([]events.Event, 0, info.State.Msgs)
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		msg, err := js.GetMsg(stream, seq)
		if err != nil {
			continue
		}

		var event events.Event
		require.NoError(t, json.Unmarshal(msg.Data, &event))
		items = append(items, event)
	}

	return items
}

func findEvent(
	items []events.Event,
	match func(events.Event) bool,
) (events.Event, bool) {
	for _, item := range items {
		if match(item) {
			return item, true
		}
	}
	return events.Event{}, false
}

func startEmbeddedNATS(t *testing.T) string {
	t.Helper()

	srv, err := natssrv.NewServer(&natssrv.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
		NoLog:     true,
		NoSigs:    true,
	})
	require.NoError(t, err)

	go srv.Start()
	require.True(t, srv.ReadyForConnections(10*time.Second), "nats server did not become ready")

	t.Cleanup(func() {
		srv.Shutdown()
		srv.WaitForShutdown()
	})

	return fmt.Sprintf("nats://%s", srv.Addr().String())
}
