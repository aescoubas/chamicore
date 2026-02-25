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

Run the built-in verification script (creates a node, assigns it to an SMD group, creates and mutates boot params, creates cloud-init payload, validates boot endpoints, boots a libvirt VM, then verifies guest login prompt/SSH reachability/cloud-init completion). `make compose-vm-up` also starts the shared Sushy emulator used by local power workflows:

```bash
./scripts/check-local-node-boot-vm.sh
```

Requirements for this step: `virsh`, `virt-install`, `qemu-img`, `ssh`, `sshpass`, `nc`, and `script`.

By default this runs in `disk` boot mode (cloud image import + cloud-init seed). Guest runtime checks use `CHAMICORE_TEST_VM_NETWORK=default` with VM credentials `chamicore` / `chamicore`.

Override as needed:

```bash
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

For true local DHCP/PXE boot through Chamicore (Kea + BSS), run in `pxe` mode:

```bash
export CHAMICORE_VM_BOOT_MODE=pxe
export CHAMICORE_TEST_VM_NETWORK=chamicore-pxe
export CHAMICORE_VM_PXE_GATEWAY_IP=172.16.10.1
./scripts/check-local-node-boot-vm.sh
```

In `pxe` mode, the script enforces the full chain evidence:
- Kea reservation for the VM MAC contains a bootscript URL.
- Kea reports an active DHCP lease for the VM MAC.
- Gateway access logs contain a successful `GET /boot/v1/bootscript?mac=<vm-mac>`.
- VM serial console shows iPXE markers and Linux kernel handoff markers.

Guest SSH/cloud-init checks remain disabled by default in `pxe` mode (`CHAMICORE_VM_GUEST_CHECKS=false`) because netboot images are typically installer-oriented. Enable them explicitly if your netboot payload provides reachable SSH/cloud-init behavior.

## 8. Tear Down

```bash
make compose-down
# or stack + VM:
make compose-vm-down
# optional for pxe mode cleanup:
# CHAMICORE_VM_BOOT_MODE=pxe CHAMICORE_VM_PXE_DESTROY_NETWORK=true make compose-vm-down
```
