//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	authclient "git.cscs.ch/openchami/chamicore-auth/pkg/client"
	authtypes "git.cscs.ch/openchami/chamicore-auth/pkg/types"
	bssclient "git.cscs.ch/openchami/chamicore-bss/pkg/client"
	bsstypes "git.cscs.ch/openchami/chamicore-bss/pkg/types"
	cloudclient "git.cscs.ch/openchami/chamicore-cloud-init/pkg/client"
	cloudtypes "git.cscs.ch/openchami/chamicore-cloud-init/pkg/types"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

type discoveryTargetSpec struct {
	Name      string   `json:"name"`
	Driver    string   `json:"driver"`
	Addresses []string `json:"addresses"`
	Enabled   bool     `json:"enabled"`
}

func TestSmoke_CRUDHappyPath(t *testing.T) {
	endpoints := smokeTestEndpoints()
	waitForAllHealthy(t, []serviceHealth{
		{name: "auth", baseURL: endpoints.auth},
		{name: "smd", baseURL: endpoints.smd},
		{name: "bss", baseURL: endpoints.bss},
		{name: "cloud-init", baseURL: endpoints.cloudInit},
		{name: "discovery", baseURL: endpoints.discovery},
		{name: "power", baseURL: endpoints.power},
	}, defaultHealthTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token := authToken()
	authSDK := authclient.New(authclient.Config{
		BaseURL: endpoints.auth,
		Token:   token,
	})
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
	cloudSDK, err := cloudclient.New(cloudclient.Config{
		BaseURL: endpoints.cloudInit,
		Token:   token,
	})
	if err != nil {
		t.Fatalf("create cloud-init client: %v", err)
	}
	discoveryHTTP := baseclient.New(baseclient.Config{
		BaseURL:    endpoints.discovery,
		Token:      token,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})

	// auth: create service account -> list service accounts
	serviceAccountName := uniqueID("smoke-sa")
	createdServiceAccount, err := authSDK.CreateServiceAccount(ctx, authtypes.CreateServiceAccountRequest{
		Name:   serviceAccountName,
		Scopes: []string{"read:components"},
	})
	if err != nil {
		t.Fatalf("auth create service account: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = authSDK.DeleteServiceAccount(cleanupCtx, createdServiceAccount.Metadata.ID)
	})

	serviceAccounts, err := authSDK.ListServiceAccounts(ctx, 200, 0)
	if err != nil {
		t.Fatalf("auth list service accounts: %v", err)
	}
	foundServiceAccount := false
	for _, item := range serviceAccounts.Items {
		if item.Metadata.ID == createdServiceAccount.Metadata.ID {
			foundServiceAccount = true
			break
		}
	}
	if !foundServiceAccount {
		t.Fatalf("auth list did not include created service account %q", createdServiceAccount.Metadata.ID)
	}

	// smd: create component -> get component
	componentID := uniqueID("node-smoke")
	createdComponent, err := smdSDK.CreateComponent(ctx, smdtypes.CreateComponentRequest{
		ID:    componentID,
		Type:  "Node",
		State: "Ready",
		Role:  "Compute",
	})
	if err != nil {
		t.Fatalf("smd create component: %v", err)
	}
	if createdComponent.Spec.ID != componentID {
		t.Fatalf("smd create returned unexpected ID: got %q want %q", createdComponent.Spec.ID, componentID)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = smdSDK.DeleteComponent(cleanupCtx, componentID)
	})

	readComponent, err := smdSDK.GetComponent(ctx, componentID)
	if err != nil {
		t.Fatalf("smd get component: %v", err)
	}
	if readComponent.Spec.ID != componentID {
		t.Fatalf("smd get returned unexpected ID: got %q want %q", readComponent.Spec.ID, componentID)
	}

	// bss: create boot params -> get boot params
	mac := uniqueMAC()
	createdBootParam, err := bssSDK.Create(ctx, bsstypes.CreateBootParamRequest{
		ComponentID: componentID,
		MAC:         &mac,
		Role:        "Compute",
		KernelURI:   "https://boot.smoke.local/vmlinuz",
		InitrdURI:   "https://boot.smoke.local/initrd.img",
		Cmdline:     "console=ttyS0",
	})
	if err != nil {
		t.Fatalf("bss create boot params: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = bssSDK.Delete(cleanupCtx, createdBootParam.Metadata.ID)
	})

	readBootParam, err := bssSDK.Get(ctx, createdBootParam.Metadata.ID)
	if err != nil {
		t.Fatalf("bss get boot params: %v", err)
	}
	if readBootParam.Spec.ComponentID != componentID {
		t.Fatalf("bss get returned unexpected component_id: got %q want %q", readBootParam.Spec.ComponentID, componentID)
	}

	// cloud-init: create payload -> get payload
	createdPayload, err := cloudSDK.Create(ctx, cloudtypes.CreatePayloadRequest{
		ComponentID: componentID,
		Role:        "Compute",
		UserData:    "#cloud-config\nhostname: smoke-node\n",
		MetaData:    json.RawMessage(`{"instance-id":"smoke-node"}`),
		VendorData:  "vendor: smoke\n",
	})
	if err != nil {
		t.Fatalf("cloud-init create payload: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = cloudSDK.Delete(cleanupCtx, createdPayload.Metadata.ID)
	})

	readPayload, err := cloudSDK.Get(ctx, createdPayload.Metadata.ID)
	if err != nil {
		t.Fatalf("cloud-init get payload: %v", err)
	}
	if readPayload.Spec.ComponentID != componentID {
		t.Fatalf("cloud-init get returned unexpected component_id: got %q want %q", readPayload.Spec.ComponentID, componentID)
	}

	// discovery: create target -> get target
	targetName := uniqueID("smoke-target")
	var createdTarget httputil.Resource[discoveryTargetSpec]
	if err := discoveryHTTP.Post(ctx, "/discovery/v1/targets", map[string]any{
		"name":      targetName,
		"driver":    "redfish",
		"addresses": []string{"192.0.2.10"},
	}, &createdTarget); err != nil {
		t.Fatalf("discovery create target: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = discoveryHTTP.Delete(cleanupCtx, "/discovery/v1/targets/"+url.PathEscape(createdTarget.Metadata.ID))
	})

	var readTarget httputil.Resource[discoveryTargetSpec]
	if err := discoveryHTTP.Get(ctx, "/discovery/v1/targets/"+url.PathEscape(createdTarget.Metadata.ID), &readTarget); err != nil {
		t.Fatalf("discovery get target: %v", err)
	}
	if readTarget.Spec.Name != targetName {
		t.Fatalf("discovery get returned unexpected target name: got %q want %q", readTarget.Spec.Name, targetName)
	}
}

func uniqueMAC() string {
	nano := uint64(time.Now().UnixNano())
	return fmt.Sprintf(
		"02:%02x:%02x:%02x:%02x:%02x",
		byte(nano),
		byte(nano>>8),
		byte(nano>>16),
		byte(nano>>24),
		byte(nano>>32),
	)
}
