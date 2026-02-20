//go:build system

package system

import (
	"encoding/json"
	"net/http"
	"testing"

	cloudclient "git.cscs.ch/openchami/chamicore-cloud-init/pkg/client"
	cloudtypes "git.cscs.ch/openchami/chamicore-cloud-init/pkg/types"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

func TestCloudInit_EndToEndServing(t *testing.T) {
	endpoints := systemEndpoints()
	waitForReadiness(t, "auth", endpoints.auth)
	waitForReadiness(t, "smd", endpoints.smd)
	waitForReadiness(t, "cloud-init", endpoints.cloudInit)

	token := authToken()
	smd := smdclient.New(smdclient.Config{
		BaseURL: endpoints.smd,
		Token:   token,
	})
	cloudInit, err := cloudclient.New(cloudclient.Config{
		BaseURL: endpoints.cloudInit,
		Token:   token,
	})
	if err != nil {
		t.Fatalf("create cloud-init client: %v", err)
	}

	componentID := uniqueID("node-system-ci")
	metaData := json.RawMessage(`{"instance-id":"system-node","local-hostname":"system-node"}`)

	ctx, cancel := systemContext(t)
	defer cancel()

	component, err := smd.CreateComponent(ctx, smdtypes.CreateComponentRequest{
		ID:    componentID,
		Type:  "Node",
		State: "Ready",
		Role:  "Compute",
	})
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if component.Spec.ID != componentID {
		t.Fatalf("created component ID mismatch: got %q want %q", component.Spec.ID, componentID)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := systemContext(t)
		defer cleanupCancel()
		_ = smd.DeleteComponent(cleanupCtx, componentID)
	})

	userData := "#cloud-config\nhostname: system-node\n"
	vendorData := "vendor: system\n"
	payload, err := cloudInit.Create(ctx, cloudtypes.CreatePayloadRequest{
		ComponentID: componentID,
		Role:        "Compute",
		UserData:    userData,
		MetaData:    metaData,
		VendorData:  vendorData,
	})
	if err != nil {
		t.Fatalf("create payload: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := systemContext(t)
		defer cleanupCancel()
		_ = cloudInit.Delete(cleanupCtx, payload.Metadata.ID)
	})

	list, err := cloudInit.List(ctx, cloudclient.ListOptions{
		ComponentID: componentID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list payloads: %v", err)
	}
	if len(list.Items) == 0 {
		t.Fatalf("expected at least one payload for component %q", componentID)
	}

	userStatus, userBody := getText(t, endpoints.cloudInit+"/cloud-init/"+componentID+"/user-data")
	if userStatus != http.StatusOK {
		t.Fatalf("user-data status: got %d want %d body=%q", userStatus, http.StatusOK, userBody)
	}
	if userBody != userData {
		t.Fatalf("user-data mismatch: got %q want %q", userBody, userData)
	}

	metaStatus, metaBody := getText(t, endpoints.cloudInit+"/cloud-init/"+componentID+"/meta-data")
	if metaStatus != http.StatusOK {
		t.Fatalf("meta-data status: got %d want %d body=%q", metaStatus, http.StatusOK, metaBody)
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(metaBody), &meta); err != nil {
		t.Fatalf("decode meta-data JSON: %v (body=%q)", err, metaBody)
	}
	if meta["instance-id"] != "system-node" {
		t.Fatalf("meta-data missing instance-id, body=%q", metaBody)
	}

	vendorStatus, vendorBody := getText(t, endpoints.cloudInit+"/cloud-init/"+componentID+"/vendor-data")
	if vendorStatus != http.StatusOK {
		t.Fatalf("vendor-data status: got %d want %d body=%q", vendorStatus, http.StatusOK, vendorBody)
	}
	if vendorBody != vendorData {
		t.Fatalf("vendor-data mismatch: got %q want %q", vendorBody, vendorData)
	}
}
