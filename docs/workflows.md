# CLI Workflow Examples

This guide provides copy-paste examples for common day-1 and day-2 operations with `chamicore-cli`.

Assumptions:
- Stack is running (`make compose-up`).
- CLI binary exists at `./bin/chamicore`.
- `jq` is available for extracting IDs/ETags from JSON output.
- Gateway endpoint is configured:

```bash
export CHAMICORE_ENDPOINT=http://localhost:8080
```

## Global Tips

- Use `-o json` for script-friendly output.
- Use `--help` on any command to inspect required flags.

```bash
./bin/chamicore --help
./bin/chamicore smd groups create --help
```

## 1. Create Components and Groups

Create two demo nodes and their interfaces:

```bash
NODE_A=node-demo-a
NODE_B=node-demo-b
MAC_A=02:00:00:00:10:01
MAC_B=02:00:00:00:10:02

./bin/chamicore smd components create --id "${NODE_A}" --type Node --state Ready --role Compute
./bin/chamicore smd components interfaces create --component-id "${NODE_A}" --mac "${MAC_A}" --ip-addrs '["172.16.10.11"]'

./bin/chamicore smd components create --id "${NODE_B}" --type Node --state Ready --role Compute
./bin/chamicore smd components interfaces create --component-id "${NODE_B}" --mac "${MAC_B}" --ip-addrs '["172.16.10.12"]'
```

Create and manage a group:

```bash
GROUP=compute-rack-r12

./bin/chamicore smd groups create \
  --name "${GROUP}" \
  --description "Compute nodes in rack R12" \
  --members "${NODE_A},${NODE_B}" \
  --tags '{"rack":"R12","purpose":"batch"}'

./bin/chamicore smd groups get "${GROUP}"
./bin/chamicore smd groups update "${GROUP}" --description "R12 production compute nodes"
./bin/chamicore smd groups remove-member "${GROUP}" "${NODE_B}"
./bin/chamicore smd groups add-member "${GROUP}" --members "${NODE_B}"
./bin/chamicore smd groups list --limit 20
```

## 2. Create and Change Boot Parameters (BSS)

Create boot parameters:

```bash
KERNEL_URI=https://boot.example.local/images/vmlinuz
INITRD_URI=https://boot.example.local/images/initrd.img

./bin/chamicore bss bootparams create \
  --component-id "${NODE_A}" \
  --mac "${MAC_A}" \
  --role Compute \
  --kernel-uri "${KERNEL_URI}" \
  --initrd-uri "${INITRD_URI}" \
  --cmdline "console=ttyS0 ip=dhcp"
```

Patch only the command line:

```bash
BOOTPARAM_ID="$(./bin/chamicore -o json bss bootparams list --component-id "${NODE_A}" --limit 1 | jq -r '.[0].metadata.id')"
./bin/chamicore bss bootparams patch "${BOOTPARAM_ID}" --cmdline "console=ttyS0 ip=dhcp rd.debug"
```

Do a full replace update with optimistic concurrency (ETag):

```bash
BOOTPARAM_ETAG="$(./bin/chamicore -o json bss bootparams get "${BOOTPARAM_ID}" | jq -r '.metadata.etag')"
./bin/chamicore bss bootparams update "${BOOTPARAM_ID}" \
  --etag "${BOOTPARAM_ETAG}" \
  --component-id "${NODE_A}" \
  --mac "${MAC_A}" \
  --role Compute \
  --kernel-uri "${KERNEL_URI}" \
  --initrd-uri "${INITRD_URI}" \
  --cmdline "console=ttyS0 ip=dhcp audit=1"
```

Validate rendered iPXE script:

```bash
MAC_A_LOWER="$(printf '%s' "${MAC_A}" | tr '[:upper:]' '[:lower:]')"
curl -fsS "http://localhost:8080/boot/v1/bootscript?mac=${MAC_A_LOWER}"
```

## 3. Create and Change Cloud-Init Payloads

Create payload:

```bash
./bin/chamicore cloud-init payloads create \
  --component-id "${NODE_A}" \
  --role Compute \
  --user-data $'#cloud-config\nhostname: node-demo-a\n' \
  --meta-data '{"instance-id":"node-demo-a","local-hostname":"node-demo-a"}' \
  --upsert
```

Patch only user-data:

```bash
PAYLOAD_ID="$(./bin/chamicore -o json cloud-init payloads list --component-id "${NODE_A}" --limit 1 | jq -r '.[0].metadata.id')"
./bin/chamicore cloud-init payloads patch "${PAYLOAD_ID}" \
  --user-data $'#cloud-config\nhostname: node-demo-a\npackage_update: true\n'
```

Full replace with ETag:

```bash
PAYLOAD_ETAG="$(./bin/chamicore -o json cloud-init payloads get "${PAYLOAD_ID}" | jq -r '.metadata.etag')"
./bin/chamicore cloud-init payloads update "${PAYLOAD_ID}" \
  --etag "${PAYLOAD_ETAG}" \
  --component-id "${NODE_A}" \
  --role Compute \
  --user-data $'#cloud-config\nhostname: node-demo-a\npackage_update: true\n' \
  --meta-data '{"instance-id":"node-demo-a","local-hostname":"node-demo-a"}' \
  --vendor-data ""
```

Validate served cloud-init content:

```bash
curl -fsS "http://localhost:8080/cloud-init/${NODE_A}/user-data"
curl -fsS "http://localhost:8080/cloud-init/${NODE_A}/meta-data"
```

## 4. Discovery Targets and Scans

Create a target:

```bash
TARGET_ID="$(
  ./bin/chamicore -o json discovery target create \
    --name rack12-ipmi \
    --driver ipmi \
    --addresses 192.0.2.10,192.0.2.11 \
    --schedule '0 */6 * * *' | jq -r '.metadata.id'
)"
```

Trigger and monitor scan:

```bash
SCAN_ID="$(./bin/chamicore -o json discovery target scan "${TARGET_ID}" | jq -r '.metadata.id')"
./bin/chamicore discovery scan status "${SCAN_ID}"
./bin/chamicore discovery scan list --limit 20
```

Ad-hoc scan example:

```bash
./bin/chamicore discovery scan trigger --driver ipmi --addresses 192.0.2.20 --data '{"port":623}'
```

## 5. Auth Administration Basics

Policies and roles:

```bash
./bin/chamicore auth policy create ops-admin smd:component write
./bin/chamicore auth policy list
./bin/chamicore auth role add-member cluster-admin user:alice
./bin/chamicore auth role list
```

Service account and credentials:

```bash
./bin/chamicore auth service-account create --name ci-bot --scopes 'smd:read,bss:write'
./bin/chamicore auth service-account list --limit 20

CREDENTIAL_ID="$(
  ./bin/chamicore -o json auth credential create \
    --name rack12-ipmi-creds \
    --type ipmi \
    --username admin \
    --secret changeme \
    --tags '{"rack":"R12"}' | jq -r '.metadata.id'
)"
./bin/chamicore auth credential get "${CREDENTIAL_ID}"
./bin/chamicore auth credential update "${CREDENTIAL_ID}" --tags '{"rack":"R12","rotated":"true"}'
```

## 6. Composite Node Workflows

Provision:

```bash
cat > /tmp/node-demo-a-user-data.yaml <<'EOF'
#cloud-config
hostname: node-demo-a
ssh_pwauth: false
EOF

./bin/chamicore node provision \
  --id node-demo-a \
  --kernel-uri "${KERNEL_URI}" \
  --initrd-uri "${INITRD_URI}" \
  --cmdline "console=ttyS0 ip=dhcp" \
  --role Compute \
  --user-data /tmp/node-demo-a-user-data.yaml
```

Dry-run decommission:

```bash
./bin/chamicore node decommission --id node-demo-a --dry-run
```

Execute decommission:

```bash
./bin/chamicore node decommission --id node-demo-a
```

## 7. Power Transitions and Status

Create one BMC and one node relationship in SMD (required for topology sync):

```bash
BMC_ID=bmc-demo-a
NODE_POWER_ID=node-power-demo-a

./bin/chamicore smd components create --id "${BMC_ID}" --type BMC --state Ready --role Management
./bin/chamicore smd components create --id "${NODE_POWER_ID}" --type Node --state Ready --role Compute --parent-id "${BMC_ID}"
./bin/chamicore smd components interfaces create --component-id "${BMC_ID}" --mac 02:00:00:00:20:01 --ip-addrs '["127.0.0.1"]'
```

Trigger power mapping sync:

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${CHAMICORE_INTERNAL_TOKEN}" \
  http://localhost:8080/power/v1/admin/mappings/sync
```

Start a dry-run power-on transition and inspect it:

```bash
TRANSITION_ID="$(
  ./bin/chamicore -o json power on --node "${NODE_POWER_ID}" --dry-run | jq -r '.transitionID'
)"

./bin/chamicore power transition get "${TRANSITION_ID}"
./bin/chamicore power transition list --limit 20
```

Run status queries by node or group:

```bash
./bin/chamicore power status --node "${NODE_POWER_ID}"
./bin/chamicore power on --group "${GROUP}" --dry-run
```

Run a non-default reset operation and wait for completion:

```bash
TRANSITION_RESET="$(
  ./bin/chamicore -o json power reset --node "${NODE_POWER_ID}" --operation GracefulShutdown | jq -r '.transitionID'
)"

./bin/chamicore power transition wait "${TRANSITION_RESET}" --timeout 120s
```
