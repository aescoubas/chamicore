//go:build smoke

package smoke

import (
	"context"
	"net/http"
	"testing"
	"time"

	bssclient "git.cscs.ch/openchami/chamicore-bss/pkg/client"
	bsstypes "git.cscs.ch/openchami/chamicore-bss/pkg/types"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

func TestSmoke_BulkEndpoints(t *testing.T) {
	endpoints := smokeTestEndpoints()
	waitForAllHealthy(t, []serviceHealth{
		{name: "auth", baseURL: endpoints.auth},
		{name: "smd", baseURL: endpoints.smd},
		{name: "bss", baseURL: endpoints.bss},
	}, defaultHealthTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	token := authToken()
	smdSDK := smdclient.New(smdclient.Config{
		BaseURL: endpoints.smd,
		Token:   token,
	})
	bssSDK, err := bssclient.New(bssclient.Config{
		BaseURL: endpoints.bss,
		Token:   token,
	})
	if err != nil {
		t.Fatalf("create bss client: %v", err)
	}

	componentA := uniqueID("bulk-node-a")
	componentB := uniqueID("bulk-node-b")
	componentC := uniqueID("bulk-node-c")
	invalidComponent := uniqueID("bulk-node-invalid")

	// SMD bulk: all valid items should be reported as created.
	smdAllValid, err := smdSDK.BulkCreateComponents(ctx, []smdtypes.CreateComponentRequest{
		{ID: componentA, Type: "Node", State: "Ready", Role: "Compute"},
		{ID: componentB, Type: "Node", State: "Ready", Role: "Compute"},
	})
	if err != nil {
		t.Fatalf("smd bulk create all-valid request failed: %v", err)
	}
	if smdAllValid.Metadata.Total != 2 || smdAllValid.Metadata.Failed != 0 {
		t.Fatalf("unexpected smd all-valid metadata: %+v", smdAllValid.Metadata)
	}
	for _, item := range smdAllValid.Items {
		if item.Status != http.StatusCreated {
			t.Fatalf("expected smd all-valid item status 201, got %d for %q", item.Status, item.ID)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = smdSDK.DeleteComponent(cleanupCtx, componentA)
		_ = smdSDK.DeleteComponent(cleanupCtx, componentB)
	})

	// SMD bulk: mixed valid/invalid should return per-item status.
	smdMixed, err := smdSDK.BulkCreateComponents(ctx, []smdtypes.CreateComponentRequest{
		{ID: componentC, Type: "Node", State: "Ready", Role: "Compute"},
		{ID: invalidComponent, Type: "NotARealType", State: "Ready", Role: "Compute"},
	})
	if err != nil {
		t.Fatalf("smd bulk create mixed request failed: %v", err)
	}
	if smdMixed.Metadata.Total != 2 || smdMixed.Metadata.Failed != 1 {
		t.Fatalf("unexpected smd mixed metadata: %+v", smdMixed.Metadata)
	}
	smdMixedStatuses := map[string]int{}
	for _, item := range smdMixed.Items {
		smdMixedStatuses[item.ID] = item.Status
	}
	if smdMixedStatuses[componentC] != http.StatusCreated {
		t.Fatalf("expected smd mixed valid item status 201, got %d", smdMixedStatuses[componentC])
	}
	if smdMixedStatuses[invalidComponent] != http.StatusUnprocessableEntity {
		t.Fatalf("expected smd mixed invalid item status 422, got %d", smdMixedStatuses[invalidComponent])
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = smdSDK.DeleteComponent(cleanupCtx, componentC)
	})

	// BSS bulk: one valid + one invalid item should be reported independently.
	validMAC := uniqueMAC()
	bssBulk, err := bssSDK.BulkSet(ctx, []bsstypes.CreateBootParamRequest{
		{
			ComponentID: componentA,
			MAC:         &validMAC,
			Role:        "Compute",
			KernelURI:   "https://bulk.smoke.local/vmlinuz",
			InitrdURI:   "https://bulk.smoke.local/initrd.img",
			Cmdline:     "console=ttyS0",
		},
		{
			ComponentID: "",
			KernelURI:   "",
			InitrdURI:   "https://bulk.smoke.local/initrd.img",
		},
	})
	if err != nil {
		t.Fatalf("bss bulk set request failed: %v", err)
	}
	if bssBulk.Metadata.Total != 2 || bssBulk.Metadata.Failed != 1 {
		t.Fatalf("unexpected bss bulk metadata: %+v", bssBulk.Metadata)
	}

	bssStatuses := map[string]int{}
	for _, item := range bssBulk.Items {
		bssStatuses[item.ID] = item.Status
	}
	if bssStatuses[componentA] != http.StatusCreated && bssStatuses[componentA] != http.StatusOK {
		t.Fatalf("expected bss valid item status 201 or 200, got %d", bssStatuses[componentA])
	}
	if bssStatuses["item-1"] != http.StatusUnprocessableEntity {
		t.Fatalf("expected bss invalid item status 422, got %d", bssStatuses["item-1"])
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		list, listErr := bssSDK.List(cleanupCtx, bssclient.ListOptions{ComponentID: componentA, Limit: 10})
		if listErr != nil || list == nil {
			return
		}
		for _, item := range list.Items {
			_ = bssSDK.Delete(cleanupCtx, item.Metadata.ID)
		}
	})
}
