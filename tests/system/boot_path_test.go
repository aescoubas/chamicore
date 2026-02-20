//go:build system

package system

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	bssclient "git.cscs.ch/openchami/chamicore-bss/pkg/client"
	bsstypes "git.cscs.ch/openchami/chamicore-bss/pkg/types"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
	smdtypes "git.cscs.ch/openchami/chamicore-smd/pkg/types"
)

func TestBootPath_EndToEnd(t *testing.T) {
	endpoints := systemEndpoints()
	waitForReadiness(t, "auth", endpoints.auth)
	waitForReadiness(t, "smd", endpoints.smd)
	waitForReadiness(t, "bss", endpoints.bss)

	token := authToken()
	smd := smdclient.New(smdclient.Config{
		BaseURL: endpoints.smd,
		Token:   token,
	})
	bss, err := bssclient.New(bssclient.Config{
		BaseURL: endpoints.bss,
		Token:   token,
	})
	if err != nil {
		t.Fatalf("create BSS client: %v", err)
	}

	componentID := uniqueID("node-system-boot")
	mac := "AA:BB:CC:11:22:33"

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

	createdBootParam, err := bss.Create(ctx, bsstypes.CreateBootParamRequest{
		ComponentID: componentID,
		MAC:         &mac,
		Role:        "Compute",
		KernelURI:   "https://boot.example.local/vmlinuz",
		InitrdURI:   "https://boot.example.local/initrd.img",
		Cmdline:     "console=ttyS0",
	})
	if err != nil {
		t.Fatalf("create boot params: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := systemContext(t)
		defer cleanupCancel()
		_ = bss.Delete(cleanupCtx, createdBootParam.Metadata.ID)
	})

	list, err := bss.List(ctx, bssclient.ListOptions{
		ComponentID: componentID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list boot params: %v", err)
	}
	if len(list.Items) == 0 {
		t.Fatalf("expected at least one boot param for component %q", componentID)
	}

	scriptURL := endpoints.bss + "/boot/v1/bootscript?mac=" + url.QueryEscape(strings.ToLower(mac))
	statusCode, script := getText(t, scriptURL)
	if statusCode != http.StatusOK {
		t.Fatalf("bootscript status: got %d want %d body=%q", statusCode, http.StatusOK, script)
	}

	for _, want := range []string{
		"#!ipxe",
		"kernel https://boot.example.local/vmlinuz console=ttyS0",
		"initrd https://boot.example.local/initrd.img",
		"boot",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("bootscript missing %q, script=%q", want, script)
		}
	}
}
