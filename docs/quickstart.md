# Chamicore Quickstart

This guide walks through a local bring-up with Docker Compose, validates service health, and exercises the CLI against the user-facing gateway endpoint.

## 1. Clone and Start the Stack

```bash
git clone --recurse-submodules git@git.cscs.ch:openchami/chamicore.git
cd chamicore
make compose-up
```

The default local entrypoint is `http://localhost:8080` (nginx gateway).

## 2. Build the CLI

```bash
mkdir -p bin
(cd services/chamicore-cli && go build -o ../../bin/chamicore ./cmd/chamicore)
./bin/chamicore --help
```

## 3. Configure the CLI Endpoint

Set endpoint/token via environment variables (highest precedence):

```bash
export CHAMICORE_ENDPOINT=http://localhost:8080
# export CHAMICORE_TOKEN=<jwt>  # only required when auth dev mode is disabled
```

You can also persist settings in `~/.chamicore/config.yaml`:

```yaml
endpoint: http://localhost:8080
output: table
auth:
  token: ""
```

Environment overrides:
- `CHAMICORE_ENDPOINT` overrides `endpoint`
- `CHAMICORE_TOKEN` overrides `auth.token`

## 4. Verify Health and Readiness

Gateway probes:

```bash
curl -fsS http://localhost:8080/health
curl -fsS http://localhost:8080/readiness
```

Per-service probes through gateway passthrough:

```bash
for svc in auth smd bss cloud-init discovery power; do
  curl -fsS "http://localhost:8080/_ops/${svc}/health" >/dev/null
  curl -fsS "http://localhost:8080/_ops/${svc}/readiness" >/dev/null
  echo "${svc}: ready"
done
```

## 5. Run Basic CLI Smoke Checks

```bash
./bin/chamicore smd components list --limit 5
./bin/chamicore smd groups list --limit 5
./bin/chamicore bss bootparams list --limit 5
./bin/chamicore cloud-init payloads list --limit 5
./bin/chamicore discovery target list --limit 5
./bin/chamicore auth policy list
./bin/chamicore power transition list --limit 5
```

## 6. Optional: Login for Non-Dev Mode

If your deployment has `CHAMICORE_*_DEV_MODE=false`, acquire and cache a token:

```bash
./bin/chamicore auth login --subject-token "$OIDC_TOKEN"
./bin/chamicore auth token
```

Or export directly:

```bash
export CHAMICORE_TOKEN=<jwt>
```

## 7. Optional: End-to-End VM Boot Validation

Run the built-in verification script (creates a node, assigns it to an SMD group, creates and mutates boot params, creates cloud-init payload, validates boot endpoints, boots a libvirt VM, validates DHCP/PXE control-plane evidence, and optionally verifies guest login prompt/SSH reachability/cloud-init completion when guest checks are enabled). `make compose-vm-up` also starts the shared Sushy emulator used by local power workflows:

```bash
./scripts/check-local-node-boot-vm.sh
```

Requirements for this step: `virsh`, `virt-install`, `qemu-img`, `ssh`, `sshpass`, `nc`, and `script`.

By default this runs in `pxe` boot mode (local Kea + BSS network boot on `chamicore-pxe`). Guest runtime checks are disabled by default in this mode (`CHAMICORE_VM_GUEST_CHECKS=false`) because installer-oriented netboot payloads usually do not expose SSH/cloud-init.

To run legacy disk-import mode instead:

```bash
export CHAMICORE_VM_BOOT_MODE=disk
export CHAMICORE_TEST_VM_NETWORK=default
export CHAMICORE_VM_SSH_USER=chamicore
export CHAMICORE_VM_SSH_PASSWORD=chamicore
```

If your guest image does not expose SSH, either switch image/credentials or disable strict guest checks:

```bash
export CHAMICORE_VM_GUEST_CHECKS=false
# optional: allow SSH/cloud-init checks without requiring serial login prompt detection
export CHAMICORE_VM_REQUIRE_CONSOLE_LOGIN_PROMPT=false
```

PXE-specific explicit overrides (optional):

```bash
export CHAMICORE_VM_BOOT_MODE=pxe
export CHAMICORE_TEST_VM_NETWORK=chamicore-pxe
export CHAMICORE_VM_PXE_GATEWAY_IP=172.16.10.1
```

In `pxe` mode, the checker now uses local gateway-hosted boot artifacts by default:
- kernel: `http://172.16.10.1:8080/pxe/vmlinuz`
- initrd: `http://172.16.10.1:8080/pxe/initrd.img`

`make compose-vm-up` stages these files from the host `/boot` tree into `shared/chamicore-deploy/nginx/pxe/`. Override sources if needed:

```bash
export CHAMICORE_VM_PXE_KERNEL_SOURCE=/path/to/vmlinuz
export CHAMICORE_VM_PXE_INITRD_SOURCE=/path/to/initrd.img
```

If host `/boot` is not readable by your user, the script can bootstrap this payload via one-time download (`CHAMICORE_VM_PXE_ALLOW_FALLBACK_DOWNLOAD=true`, default). Runtime PXE fetch still uses the local gateway URLs above.

Default local PXE-related host ports:
- `CHAMICORE_SUSHY_HOST_PORT=8001` (sushy-tools)
- `CHAMICORE_KEA_PXE_CONTROL_PORT=18000` (Kea shim API endpoint used by kea-sync/checker)
- `CHAMICORE_KEA_PXE_CTRL_AGENT_PORT=18001` (Kea control-agent inside `kea-pxe`)
- `CHAMICORE_KEASYNC_SYNC_ON_STARTUP_PXE=true` and `CHAMICORE_KEASYNC_SYNC_INTERVAL_PXE=10s` (fast PXE reservation/boot-option reconciliation)
- `CHAMICORE_VM_PXE_DISABLE_CONFLICTING_DHCP_NETWORKS=true` (temporarily stop other active libvirt DHCP networks to free UDP/67)
- `CHAMICORE_VM_PXE_RESTORE_CONFLICTING_DHCP_NETWORKS=true` (restore those stopped networks on `make compose-vm-down`)

In `pxe` mode, the script validates DHCP/PXE control-plane evidence:
- Kea reservation for the VM MAC contains a bootscript URL.
- Kea reports an active DHCP lease for the VM MAC.
- Gateway access logs contain a successful `GET /boot/v1/bootscript?mac=<vm-mac>`.

By default, serial-console chain markers are best-effort because some firmware/ROM combinations do not emit reliable output. To enforce strict console validation, set:

```bash
export CHAMICORE_VM_PXE_REQUIRE_CONSOLE_CHAIN=true
```

In strict mode, the checker requires Linux-kernel handoff markers on serial output and still enforces hard gateway/Kea checks. Explicit iPXE markers are preferred, but when firmware omits them the chain is accepted if gateway bootscript fetch succeeded and Linux markers are present.

Serial output is captured from VM creation into:
- `${CHAMICORE_VM_SERIAL_LOG:-shared/chamicore-deploy/.artifacts/libvirt/<vm-name>-serial.log}`
- capture process pid file: `${CHAMICORE_VM_SERIAL_CAPTURE_PID_FILE:-shared/chamicore-deploy/.artifacts/libvirt/<vm-name>-serial-capture.pid}`

When PXE debug capture is enabled (`CHAMICORE_VM_PXE_CAPTURE_DEBUG_EVIDENCE=true`, default), the checker also writes gateway/Kea/serial snapshots under:
- `${CHAMICORE_VM_PXE_DEBUG_ARTIFACTS_DIR:-.artifacts/check-local-node-boot-vm}/`

Guest SSH/cloud-init checks remain disabled by default in `pxe` mode (`CHAMICORE_VM_GUEST_CHECKS=false`) because netboot images are typically installer-oriented. Enable them explicitly if your netboot payload provides reachable SSH/cloud-init behavior.

Troubleshooting common PXE bind failures:
- `Address already in use` on UDP `:67`:
  another DHCP server is bound in host namespace. By default, PXE mode temporarily stops other active libvirt DHCP networks. If you disabled this (`CHAMICORE_VM_PXE_DISABLE_CONFLICTING_DHCP_NETWORKS=false`), stop conflicts manually and ensure `virsh net-dumpxml <network>` has no `<dhcp>` block in PXE mode.
- `Address already in use` on `:18000` or `:18001`:
  override `CHAMICORE_KEA_PXE_CONTROL_PORT` or `CHAMICORE_KEA_PXE_CTRL_AGENT_PORT` in `.env` to free ports.
- `Address already in use` on `:8001`:
  sushy-tools host port collision; set `CHAMICORE_SUSHY_HOST_PORT` to an unused port.
- VM shows no PXE/DHCP traffic:
  ensure a PXE-capable NIC model is used (`CHAMICORE_VM_PXE_NIC_MODEL=e1000` is the local default) and keep the iPXE ROM auto-detected (or set `CHAMICORE_VM_PXE_ROMFILE` explicitly, for example `/usr/lib/ipxe/qemu/pxe-e1000.rom`).
- strict console chain fails:
  inspect `${CHAMICORE_VM_SERIAL_LOG}` first. If serial output is empty but gateway `bootscript` fetch and Kea lease checks pass, firmware in this environment is not emitting reliable serial markers; keep `CHAMICORE_VM_PXE_REQUIRE_CONSOLE_CHAIN=false` for portability.
  for deeper iPXE-side debugging, set `CHAMICORE_BSS_BOOTSCRIPT_DEBUG=true` so generated bootscripts print explicit markers before kernel/initrd/boot and on boot failure.

## 8. Tear Down

```bash
make compose-down
# or stack + VM:
make compose-vm-down
# optional network cleanup:
# CHAMICORE_VM_BOOT_MODE=pxe CHAMICORE_VM_PXE_DESTROY_NETWORK=true make compose-vm-down
```
