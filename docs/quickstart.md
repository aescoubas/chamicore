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

Run the built-in verification script (creates a node, boot params, cloud-init payload, validates boot endpoints, then boots a libvirt VM). `make compose-vm-up` also starts the shared Sushy emulator used by local power workflows:

```bash
./scripts/check-local-node-boot-vm.sh
```

Requirements for this step: `virsh`, `virt-install`, and `qemu-img`.

## 8. Tear Down

```bash
make compose-down
# or stack + VM:
make compose-vm-down
```
