//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	powerclient "git.cscs.ch/openchami/chamicore-power/pkg/client"
	powertypes "git.cscs.ch/openchami/chamicore-power/pkg/types"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

func TestSmoke_PowerTransitionDryRunWorkflow(t *testing.T) {
	endpoints := smokeTestEndpoints()
	waitForAllHealthy(t, []serviceHealth{
		{name: "auth", baseURL: endpoints.auth},
		{name: "smd", baseURL: endpoints.smd},
		{name: "bss", baseURL: endpoints.bss},
		{name: "cloud-init", baseURL: endpoints.cloudInit},
		{name: "discovery", baseURL: endpoints.discovery},
		{name: "power", baseURL: endpoints.power},
	}, defaultHealthTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token := authToken()
	smdSDK := smdclient.New(smdclient.Config{
		BaseURL: endpoints.smd,
		Token:   token,
	})
	powerSDK, err := powerclient.New(powerclient.Config{
		BaseURL: endpoints.power,
		Token:   token,
	})
	if err != nil {
		t.Fatalf("create power client: %v", err)
	}

	bmcID := uniqueID("bmc-power-smoke")
	nodeID := uniqueID("node-power-smoke")
	parentID := bmcID

	_, err = smdSDK.CreateComponent(ctx, smdtypes.CreateComponentRequest{
		ID:    bmcID,
		Type:  "BMC",
		State: "Ready",
		Role:  "Management",
	})
	if err != nil {
		t.Fatalf("smd create bmc component: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = smdSDK.DeleteComponent(cleanupCtx, bmcID)
	})

	_, err = smdSDK.CreateComponent(ctx, smdtypes.CreateComponentRequest{
		ID:       nodeID,
		Type:     "Node",
		State:    "Ready",
		Role:     "Compute",
		ParentID: &parentID,
	})
	if err != nil {
		t.Fatalf("smd create node component: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = smdSDK.DeleteComponent(cleanupCtx, nodeID)
	})

	iface, err := smdSDK.CreateEthernetInterface(ctx, smdtypes.CreateEthernetInterfaceRequest{
		ComponentID: bmcID,
		MACAddr:     uniqueMAC(),
		IPAddrs:     json.RawMessage(`["127.0.0.1"]`),
	})
	if err != nil {
		t.Fatalf("smd create bmc interface: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = smdSDK.DeleteEthernetInterface(cleanupCtx, iface.Metadata.ID)
	})

	if _, err := powerSDK.TriggerMappingSync(ctx); err != nil {
		t.Fatalf("power trigger mapping sync: %v", err)
	}

	transition, err := powerSDK.ActionOn(ctx, powertypes.ActionRequest{
		RequestID: uniqueID("power-dryrun"),
		Nodes:     []string{nodeID},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("power action on (dry-run): %v", err)
	}

	if transition.Spec.State != powertypes.TransitionStatePlanned {
		t.Fatalf(
			"unexpected transition state: got %q want %q",
			transition.Spec.State,
			powertypes.TransitionStatePlanned,
		)
	}
	if len(transition.Spec.Tasks) != 1 {
		t.Fatalf("unexpected transition task count: got %d want 1", len(transition.Spec.Tasks))
	}
	if transition.Spec.Tasks[0].NodeID != nodeID {
		t.Fatalf("unexpected transition node id: got %q want %q", transition.Spec.Tasks[0].NodeID, nodeID)
	}
	if transition.Spec.Tasks[0].State != powertypes.TaskStatePlanned {
		t.Fatalf("unexpected task state: got %q want %q", transition.Spec.Tasks[0].State, powertypes.TaskStatePlanned)
	}

	status, err := powerSDK.GetPowerStatus(ctx, powerclient.PowerStatusOptions{
		Nodes: []string{nodeID},
	})
	if err != nil {
		t.Fatalf("power get status: %v", err)
	}
	if status.Spec.Total != 1 {
		t.Fatalf("unexpected power status total: got %d want 1", status.Spec.Total)
	}
	if len(status.Spec.NodeStatuses) != 1 {
		t.Fatalf("unexpected power node status count: got %d want 1", len(status.Spec.NodeStatuses))
	}
	if status.Spec.NodeStatuses[0].NodeID != nodeID {
		t.Fatalf("unexpected power status node id: got %q want %q", status.Spec.NodeStatuses[0].NodeID, nodeID)
	}
	if status.Spec.NodeStatuses[0].State != powertypes.TaskStatePlanned {
		t.Fatalf(
			"unexpected power status state: got %q want %q",
			status.Spec.NodeStatuses[0].State,
			powertypes.TaskStatePlanned,
		)
	}
}
