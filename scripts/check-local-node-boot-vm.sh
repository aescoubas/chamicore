#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

CLI_BIN="${CHAMICORE_CLI_BIN:-${REPO_ROOT}/bin/chamicore}"
SMD_ENDPOINT="${CHAMICORE_SMD_ENDPOINT:-http://localhost:27779}"
BSS_ENDPOINT="${CHAMICORE_BSS_ENDPOINT:-http://localhost:27778}"
CLOUDINIT_ENDPOINT="${CHAMICORE_CLOUDINIT_ENDPOINT:-http://localhost:27777}"
VM_NAME="${CHAMICORE_VM_NAME:-chamicore-devvm}"

NODE_ID="${CHAMICORE_TEST_NODE_ID:-node-demo-$(date +%s)}"
KERNEL_URI="${CHAMICORE_TEST_KERNEL_URI:-https://boot.example.local/vmlinuz}"
INITRD_URI="${CHAMICORE_TEST_INITRD_URI:-https://boot.example.local/initrd.img}"
CMDLINE="${CHAMICORE_TEST_CMDLINE:-console=ttyS0}"
ROLE="${CHAMICORE_TEST_ROLE:-Compute}"
INTERFACE_IPS="${CHAMICORE_TEST_IPS_JSON:-[\"172.16.10.50\"]}"
SKIP_COMPOSE_UP="${CHAMICORE_SKIP_COMPOSE_UP:-false}"
READINESS_TIMEOUT_SECONDS="${CHAMICORE_READINESS_TIMEOUT_SECONDS:-60}"
CURL_MAX_TIME="${CHAMICORE_CURL_MAX_TIME:-5}"

log() {
  printf '[check-local-node-boot-vm] %s\n' "$*"
}

fail() {
  printf '[check-local-node-boot-vm] error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || fail "missing required command: ${cmd}"
}

is_true() {
  case "${1,,}" in
    1|true|yes|on) return 0 ;;
  esac
  return 1
}

contains_or_fail() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "${haystack}" != *"${needle}"* ]]; then
    fail "${message}. missing '${needle}'"
  fi
}

generate_mac() {
  local raw
  raw="$(od -An -N5 -tx1 /dev/urandom | tr -d ' \n')"
  printf '02:%s:%s:%s:%s:%s\n' \
    "${raw:0:2}" \
    "${raw:2:2}" \
    "${raw:4:2}" \
    "${raw:6:2}" \
    "${raw:8:2}"
}

MAC="${CHAMICORE_TEST_MAC:-$(generate_mac)}"
LOWER_MAC="$(printf '%s' "${MAC}" | tr '[:upper:]' '[:lower:]')"
USER_DATA="$(printf '#cloud-config\nhostname: %s\n' "${NODE_ID}")"
META_DATA="$(printf '{"instance-id":"%s","local-hostname":"%s"}' "${NODE_ID}" "${NODE_ID}")"

check_readiness() {
  local name="$1"
  local endpoint="$2"
  local url="${endpoint}/readiness"
  local deadline
  deadline="$((SECONDS + READINESS_TIMEOUT_SECONDS))"

  log "checking ${name} readiness: ${url}"
  while (( SECONDS < deadline )); do
    if curl --max-time "${CURL_MAX_TIME}" -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  fail "${name} readiness check failed within ${READINESS_TIMEOUT_SECONDS}s (${url})"
}

check_api() {
  local name="$1"
  local url="$2"
  local timeout="$3"

  log "checking ${name} api: ${url}"
  curl --max-time "${timeout}" -fsS "${url}" >/dev/null || fail "${name} api check failed (${url})"
}

ensure_cli() {
  if [[ -x "${CLI_BIN}" ]]; then
    return 0
  fi

  log "building CLI binary: ${CLI_BIN}"
  require_cmd go
  mkdir -p "$(dirname -- "${CLI_BIN}")"
  (
    cd "${REPO_ROOT}/services/chamicore-cli"
    go build -o "${CLI_BIN}" ./cmd/chamicore
  )
}

run_cli() {
  local endpoint="$1"
  shift
  CHAMICORE_ENDPOINT="${endpoint}" "${CLI_BIN}" "$@"
}

create_resources() {
  log "creating SMD component ${NODE_ID}"
  run_cli "${SMD_ENDPOINT}" smd components create \
    --id "${NODE_ID}" \
    --type Node \
    --state Ready \
    --role "${ROLE}"

  log "creating SMD interface with MAC ${MAC}"
  run_cli "${SMD_ENDPOINT}" smd components interfaces create \
    --component-id "${NODE_ID}" \
    --mac "${MAC}" \
    --ip-addrs "${INTERFACE_IPS}"

  log "creating BSS boot parameters"
  run_cli "${BSS_ENDPOINT}" bss bootparams create \
    --component-id "${NODE_ID}" \
    --mac "${MAC}" \
    --role "${ROLE}" \
    --kernel-uri "${KERNEL_URI}" \
    --initrd-uri "${INITRD_URI}" \
    --cmdline "${CMDLINE}"

  log "creating Cloud-Init payload"
  run_cli "${CLOUDINIT_ENDPOINT}" cloud-init payloads create \
    --component-id "${NODE_ID}" \
    --role "${ROLE}" \
    --user-data "${USER_DATA}" \
    --meta-data "${META_DATA}" \
    --upsert
}

validate_boot_path() {
  local bootscript_url="${BSS_ENDPOINT}/boot/v1/bootscript?mac=${LOWER_MAC}"
  log "validating BSS bootscript: ${bootscript_url}"
  local bootscript
  bootscript="$(curl --max-time "${CURL_MAX_TIME}" -fsS "${bootscript_url}")"

  contains_or_fail "${bootscript}" "#!ipxe" "invalid bootscript"
  contains_or_fail "${bootscript}" "kernel ${KERNEL_URI} ${CMDLINE}" "bootscript kernel line mismatch"
  contains_or_fail "${bootscript}" "initrd ${INITRD_URI}" "bootscript initrd line mismatch"
  contains_or_fail "${bootscript}" "boot" "bootscript missing boot directive"

  local user_data_url="${CLOUDINIT_ENDPOINT}/cloud-init/${NODE_ID}/user-data"
  log "validating Cloud-Init user-data: ${user_data_url}"
  local user_data_resp
  user_data_resp="$(curl --max-time "${CURL_MAX_TIME}" -fsS "${user_data_url}")"
  contains_or_fail "${user_data_resp}" "#cloud-config" "invalid cloud-init user-data"
  contains_or_fail "${user_data_resp}" "hostname: ${NODE_ID}" "cloud-init user-data hostname mismatch"

  local meta_data_url="${CLOUDINIT_ENDPOINT}/cloud-init/${NODE_ID}/meta-data"
  log "validating Cloud-Init meta-data: ${meta_data_url}"
  local meta_data_resp
  meta_data_resp="$(curl --max-time "${CURL_MAX_TIME}" -fsS "${meta_data_url}")"
  local meta_data_minified
  meta_data_minified="$(printf '%s' "${meta_data_resp}" | tr -d '[:space:]')"
  contains_or_fail "${meta_data_minified}" "\"instance-id\":\"${NODE_ID}\"" "cloud-init meta-data instance-id mismatch"
}

boot_vm() {
  log "starting libvirt VM via make compose-vm-up"
  (cd "${REPO_ROOT}" && CHAMICORE_VM_SKIP_COMPOSE=true make compose-vm-up)

  local state
  state="$(virsh domstate "${VM_NAME}" 2>/dev/null | tr -d '\r' | tr '[:upper:]' '[:lower:]')"
  if [[ "${state}" != *"running"* ]]; then
    fail "libvirt domain ${VM_NAME} is not running (state: ${state:-unknown})"
  fi

  log "libvirt domain ${VM_NAME} state: ${state}"
  virsh dominfo "${VM_NAME}"
}

main() {
  require_cmd curl
  require_cmd make
  require_cmd virsh
  require_cmd od
  ensure_cli

  if ! is_true "${SKIP_COMPOSE_UP}"; then
    log "ensuring compose stack is up"
    (cd "${REPO_ROOT}" && make compose-up)
  fi

  check_readiness "smd" "${SMD_ENDPOINT}"
  check_readiness "bss" "${BSS_ENDPOINT}"
  check_readiness "cloud-init" "${CLOUDINIT_ENDPOINT}"
  check_api "smd" "${SMD_ENDPOINT}/hsm/v2/State/Components?limit=1" "${CURL_MAX_TIME}"
  check_api "bss" "${BSS_ENDPOINT}/boot/v1/bootparams?limit=1" "${CURL_MAX_TIME}"
  check_api "cloud-init" "${CLOUDINIT_ENDPOINT}/cloud-init/payloads?limit=1" "${CURL_MAX_TIME}"

  create_resources
  validate_boot_path
  boot_vm

  log "success"
  log "node_id=${NODE_ID}"
  log "mac=${MAC}"
  log "vm_name=${VM_NAME}"
}

main "$@"
