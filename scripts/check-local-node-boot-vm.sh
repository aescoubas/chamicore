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
GROUP_NAME="${CHAMICORE_TEST_GROUP_NAME:-group-${NODE_ID}}"
GROUP_TAGS="${CHAMICORE_TEST_GROUP_TAGS:-{\"rack\":\"R12\",\"purpose\":\"vm-e2e\"}}"
GROUP_DESCRIPTION_INITIAL="${CHAMICORE_TEST_GROUP_DESCRIPTION_INITIAL:-VM E2E validation group}"
GROUP_DESCRIPTION_UPDATED="${CHAMICORE_TEST_GROUP_DESCRIPTION_UPDATED:-VM E2E validation group (updated)}"
BOOTPARAM_PATCH_CMDLINE="${CHAMICORE_TEST_BOOTPARAM_PATCH_CMDLINE:-console=ttyS0 ip=dhcp rd.debug}"
BOOTPARAM_UPDATED_CMDLINE="${CHAMICORE_TEST_BOOTPARAM_UPDATED_CMDLINE:-console=ttyS0 ip=dhcp rd.debug audit=1}"
BOOTPARAM_DISCOVERY_TIMEOUT_SECONDS="${CHAMICORE_TEST_BOOTPARAM_DISCOVERY_TIMEOUT_SECONDS:-15}"
VM_NETWORK="${CHAMICORE_TEST_VM_NETWORK:-default}"
VM_RECREATE="${CHAMICORE_TEST_VM_RECREATE:-true}"
VM_GUEST_CHECKS="${CHAMICORE_VM_GUEST_CHECKS:-true}"
VM_REQUIRE_CONSOLE_LOGIN_PROMPT="${CHAMICORE_VM_REQUIRE_CONSOLE_LOGIN_PROMPT:-true}"
VM_IP_TIMEOUT_SECONDS="${CHAMICORE_VM_IP_TIMEOUT_SECONDS:-180}"
VM_LOGIN_PROMPT_TIMEOUT_SECONDS="${CHAMICORE_VM_LOGIN_PROMPT_TIMEOUT_SECONDS:-60}"
VM_SSH_TIMEOUT_SECONDS="${CHAMICORE_VM_SSH_TIMEOUT_SECONDS:-180}"
VM_SSH_LOGIN_TIMEOUT_SECONDS="${CHAMICORE_VM_SSH_LOGIN_TIMEOUT_SECONDS:-180}"
VM_SSH_PORT="${CHAMICORE_VM_SSH_PORT:-22}"
VM_SSH_USER="${CHAMICORE_VM_SSH_USER:-${CHAMICORE_VM_CLOUD_INIT_USER:-chamicore}}"
VM_SSH_PASSWORD="${CHAMICORE_VM_SSH_PASSWORD:-${CHAMICORE_VM_CLOUD_INIT_PASSWORD:-chamicore}}"
VM_CLOUD_INIT_ENABLE="${CHAMICORE_VM_CLOUD_INIT_ENABLE:-true}"
VM_CLOUD_INIT_USER="${CHAMICORE_VM_CLOUD_INIT_USER:-${VM_SSH_USER}}"
VM_CLOUD_INIT_PASSWORD="${CHAMICORE_VM_CLOUD_INIT_PASSWORD:-${VM_SSH_PASSWORD}}"
VM_SSH_KNOWN_HOSTS="${CHAMICORE_VM_SSH_KNOWN_HOSTS:-${REPO_ROOT}/.artifacts/known_hosts-vm}"

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
BOOTPARAM_ID=""
BOOTPARAM_ETAG=""
EFFECTIVE_CMDLINE="${CMDLINE}"
VM_IP=""

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
  local output
  if ! output="$(CHAMICORE_ENDPOINT="${endpoint}" "${CLI_BIN}" "$@" 2>&1)"; then
    printf '%s\n' "${output}" >&2
    fail "cli command failed: ${CLI_BIN} $*"
  fi

  if printf '%s\n' "${output}" | grep -Eiq 'HTTP [45][0-9]{2}:|accepts [0-9]+ arg\(s\)|flag needs an argument'; then
    printf '%s\n' "${output}" >&2
    fail "cli command returned API/validation error: ${CLI_BIN} $*"
  fi

  printf '%s\n' "${output}"
}

require_json_expr() {
  local json="$1"
  local expr="$2"
  local message="$3"

  if ! printf '%s' "${json}" | jq -e "${expr}" >/dev/null; then
    fail "${message}"
  fi
}

require_json_expr_arg() {
  local json="$1"
  local expr="$2"
  local arg_name="$3"
  local arg_value="$4"
  local message="$5"

  if ! printf '%s' "${json}" | jq -e --arg "${arg_name}" "${arg_value}" "${expr}" >/dev/null; then
    fail "${message}"
  fi
}

resolve_bootparam_id() {
  local bootparam_list
  bootparam_list="$(run_cli "${BSS_ENDPOINT}" -o json bss bootparams list --component-id "${NODE_ID}" --limit 1)"
  BOOTPARAM_ID="$(printf '%s' "${bootparam_list}" | jq -r '.[0].metadata.id')"
  if [[ "${BOOTPARAM_ID}" == "null" ]]; then
    BOOTPARAM_ID=""
  fi
}

wait_for_bootparam_id() {
  local deadline
  deadline="$((SECONDS + BOOTPARAM_DISCOVERY_TIMEOUT_SECONDS))"

  while (( SECONDS < deadline )); do
    resolve_bootparam_id
    if [[ -n "${BOOTPARAM_ID}" ]]; then
      return 0
    fi
    sleep 1
  done
  return 1
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

  # BSS may auto-sync and create boot params from SMD interfaces; prefer existing record if present.
  wait_for_bootparam_id || true
  if [[ -z "${BOOTPARAM_ID}" ]]; then
    log "creating BSS boot parameters"
    run_cli "${BSS_ENDPOINT}" bss bootparams create \
      --component-id "${NODE_ID}" \
      --mac "${MAC}" \
      --role "${ROLE}" \
      --kernel-uri "${KERNEL_URI}" \
      --initrd-uri "${INITRD_URI}" \
      --cmdline "${CMDLINE}"
    wait_for_bootparam_id || true
  else
    log "using existing BSS boot parameters for ${NODE_ID}: ${BOOTPARAM_ID}"
  fi

  [[ -n "${BOOTPARAM_ID}" ]] || fail "unable to resolve boot parameter id for ${NODE_ID}"

  log "creating Cloud-Init payload"
  run_cli "${CLOUDINIT_ENDPOINT}" cloud-init payloads create \
    --component-id "${NODE_ID}" \
    --role "${ROLE}" \
    --user-data "${USER_DATA}" \
    --meta-data "${META_DATA}" \
    --upsert
}

exercise_group_workflow() {
  log "creating SMD group ${GROUP_NAME}"
  run_cli "${SMD_ENDPOINT}" smd groups create \
    --name "${GROUP_NAME}" \
    --description "${GROUP_DESCRIPTION_INITIAL}" \
    --members "${NODE_ID}" \
    --tags "${GROUP_TAGS}"

  log "verifying group ${GROUP_NAME} contains ${NODE_ID}"
  local group_json
  group_json="$(run_cli "${SMD_ENDPOINT}" -o json smd groups get "${GROUP_NAME}")"
  require_json_expr_arg "${group_json}" '.spec.members | index($node) != null' "node" "${NODE_ID}" "group does not contain expected member"

  log "updating group ${GROUP_NAME} description"
  run_cli "${SMD_ENDPOINT}" smd groups update "${GROUP_NAME}" --description "${GROUP_DESCRIPTION_UPDATED}"

  group_json="$(run_cli "${SMD_ENDPOINT}" -o json smd groups get "${GROUP_NAME}")"
  require_json_expr_arg "${group_json}" '.spec.description == $description' "description" "${GROUP_DESCRIPTION_UPDATED}" "group description did not update"

  log "exercising group member remove/add operations"
  run_cli "${SMD_ENDPOINT}" smd groups remove-member "${GROUP_NAME}" "${NODE_ID}"
  run_cli "${SMD_ENDPOINT}" smd groups add-member "${GROUP_NAME}" --members "${NODE_ID}"

  group_json="$(run_cli "${SMD_ENDPOINT}" -o json smd groups get "${GROUP_NAME}")"
  require_json_expr_arg "${group_json}" '.spec.members | index($node) != null' "node" "${NODE_ID}" "group membership was not restored after add-member"
}

exercise_bootparam_workflow() {
  log "patching boot parameter ${BOOTPARAM_ID}"
  run_cli "${BSS_ENDPOINT}" bss bootparams patch "${BOOTPARAM_ID}" --cmdline "${BOOTPARAM_PATCH_CMDLINE}"

  local bootparam_json
  bootparam_json="$(run_cli "${BSS_ENDPOINT}" -o json bss bootparams get "${BOOTPARAM_ID}")"
  require_json_expr_arg "${bootparam_json}" '.spec.cmdline == $cmdline' "cmdline" "${BOOTPARAM_PATCH_CMDLINE}" "boot parameter patch did not update cmdline"

  BOOTPARAM_ETAG="$(printf '%s' "${bootparam_json}" | jq -r '.metadata.etag')"
  [[ -n "${BOOTPARAM_ETAG}" && "${BOOTPARAM_ETAG}" != "null" ]] || fail "unable to resolve boot parameter etag for ${BOOTPARAM_ID}"

  log "performing full boot parameter update for ${BOOTPARAM_ID}"
  run_cli "${BSS_ENDPOINT}" bss bootparams update "${BOOTPARAM_ID}" \
    --etag "${BOOTPARAM_ETAG}" \
    --component-id "${NODE_ID}" \
    --mac "${MAC}" \
    --role "${ROLE}" \
    --kernel-uri "${KERNEL_URI}" \
    --initrd-uri "${INITRD_URI}" \
    --cmdline "${BOOTPARAM_UPDATED_CMDLINE}"

  bootparam_json="$(run_cli "${BSS_ENDPOINT}" -o json bss bootparams get "${BOOTPARAM_ID}")"
  require_json_expr_arg "${bootparam_json}" '.spec.cmdline == $cmdline' "cmdline" "${BOOTPARAM_UPDATED_CMDLINE}" "boot parameter update did not persist expected cmdline"

  EFFECTIVE_CMDLINE="${BOOTPARAM_UPDATED_CMDLINE}"
}

validate_boot_path() {
  local bootscript_url="${BSS_ENDPOINT}/boot/v1/bootscript?mac=${LOWER_MAC}"
  log "validating BSS bootscript: ${bootscript_url}"
  local bootscript
  bootscript="$(curl --max-time "${CURL_MAX_TIME}" -fsS "${bootscript_url}")"

  contains_or_fail "${bootscript}" "#!ipxe" "invalid bootscript"
  contains_or_fail "${bootscript}" "kernel ${KERNEL_URI} ${EFFECTIVE_CMDLINE}" "bootscript kernel line mismatch"
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
  (
    cd "${REPO_ROOT}" && \
      CHAMICORE_VM_SKIP_COMPOSE=true \
      CHAMICORE_VM_NETWORK="${VM_NETWORK}" \
      CHAMICORE_VM_RECREATE="${VM_RECREATE}" \
      CHAMICORE_VM_CLOUD_INIT_ENABLE="${VM_CLOUD_INIT_ENABLE}" \
      CHAMICORE_VM_CLOUD_INIT_USER="${VM_CLOUD_INIT_USER}" \
      CHAMICORE_VM_CLOUD_INIT_PASSWORD="${VM_CLOUD_INIT_PASSWORD}" \
      make compose-vm-up
  )

  local state
  state="$(virsh domstate "${VM_NAME}" 2>/dev/null | tr -d '\r' | tr '[:upper:]' '[:lower:]')"
  if [[ "${state}" != *"running"* ]]; then
    fail "libvirt domain ${VM_NAME} is not running (state: ${state:-unknown})"
  fi

  log "libvirt domain ${VM_NAME} state: ${state}"
  virsh dominfo "${VM_NAME}"
}

resolve_vm_ip() {
  local deadline
  deadline="$((SECONDS + VM_IP_TIMEOUT_SECONDS))"
  local vm_mac=""
  local vm_net_source=""
  vm_mac="$(virsh domiflist "${VM_NAME}" 2>/dev/null | awk 'NR>2 && $5 != "" {print tolower($5); exit}')"
  vm_net_source="$(virsh domiflist "${VM_NAME}" 2>/dev/null | awk 'NR>2 && $3 != "" {print $3; exit}')"

  log "resolving VM IP for ${VM_NAME} (network=${VM_NETWORK})"
  while (( SECONDS < deadline )); do
    VM_IP="$(
      virsh domifaddr "${VM_NAME}" --source lease 2>/dev/null | \
        awk '/ipv4/ {print $4}' | \
        head -n1 | \
        cut -d/ -f1 || true
    )"
    if [[ -z "${VM_IP}" ]]; then
      VM_IP="$(
        virsh domifaddr "${VM_NAME}" --source agent 2>/dev/null | \
          awk '/ipv4/ {print $4}' | \
          head -n1 | \
          cut -d/ -f1 || true
      )"
    fi
    if [[ -z "${VM_IP}" && -n "${vm_mac}" && -n "${vm_net_source}" && "${vm_net_source}" != "-" ]]; then
      VM_IP="$(
        virsh net-dhcp-leases "${vm_net_source}" 2>/dev/null | \
          awk -v mac="${vm_mac}" 'tolower($2) == mac {print $4; exit}' | \
          cut -d/ -f1 || true
      )"
    fi

    if [[ -n "${VM_IP}" ]]; then
      log "resolved VM IP: ${VM_IP}"
      return 0
    fi
    sleep 2
  done

  fail "unable to resolve VM IP for ${VM_NAME} within ${VM_IP_TIMEOUT_SECONDS}s (network=${VM_NETWORK})"
}

check_console_login_prompt() {
  local output=""
  output="$(
    timeout "${VM_LOGIN_PROMPT_TIMEOUT_SECONDS}s" \
      bash -lc "printf '\n' | script -qec 'virsh console ${VM_NAME}' /dev/null" 2>&1 || true
  )"

  if printf '%s\n' "${output}" | grep -Eiq 'login:|localhost login|ubuntu login'; then
    log "detected guest login prompt on serial console"
    return 0
  fi

  if is_true "${VM_REQUIRE_CONSOLE_LOGIN_PROMPT}"; then
    printf '%s\n' "${output}" >&2
    fail "did not detect a login prompt on VM serial console within ${VM_LOGIN_PROMPT_TIMEOUT_SECONDS}s"
  fi

  log "console login prompt not detected (continuing: CHAMICORE_VM_REQUIRE_CONSOLE_LOGIN_PROMPT=${VM_REQUIRE_CONSOLE_LOGIN_PROMPT})"
}

wait_for_ssh_port() {
  local deadline
  deadline="$((SECONDS + VM_SSH_TIMEOUT_SECONDS))"

  log "waiting for SSH port ${VM_SSH_PORT} on ${VM_IP}"
  while (( SECONDS < deadline )); do
    if nc -z -w 2 "${VM_IP}" "${VM_SSH_PORT}" >/dev/null 2>&1; then
      log "SSH port ${VM_SSH_PORT} is reachable on ${VM_IP}"
      return 0
    fi
    sleep 2
  done

  fail "SSH port ${VM_SSH_PORT} on ${VM_IP} did not become reachable within ${VM_SSH_TIMEOUT_SECONDS}s"
}

run_vm_ssh() {
  local remote_cmd="$1"
  mkdir -p "$(dirname -- "${VM_SSH_KNOWN_HOSTS}")"
  touch "${VM_SSH_KNOWN_HOSTS}"
  local -a ssh_opts=(
    -o StrictHostKeyChecking=no
    -o UserKnownHostsFile="${VM_SSH_KNOWN_HOSTS}"
    -o ConnectTimeout=5
    -o NumberOfPasswordPrompts=1
    -p "${VM_SSH_PORT}"
  )

  sshpass -p "${VM_SSH_PASSWORD}" ssh \
    "${ssh_opts[@]}" \
    -o BatchMode=no \
    -o PreferredAuthentications=password \
    -o PasswordAuthentication=yes \
    -o PubkeyAuthentication=no \
    -o KbdInteractiveAuthentication=no \
    -o IdentitiesOnly=yes \
    "${VM_SSH_USER}@${VM_IP}" \
    "${remote_cmd}"
}

wait_for_ssh_login() {
  local deadline
  deadline="$((SECONDS + VM_SSH_LOGIN_TIMEOUT_SECONDS))"
  local login_output=""

  log "waiting for SSH login (${VM_SSH_USER}@${VM_IP})"
  while (( SECONDS < deadline )); do
    if login_output="$(run_vm_ssh 'echo guest_login_ok' 2>/dev/null)"; then
      if [[ "${login_output}" == *"guest_login_ok"* ]]; then
        log "SSH login verified (${VM_SSH_USER}@${VM_IP})"
        return 0
      fi
    fi
    sleep 2
  done

  fail "SSH login did not succeed within ${VM_SSH_LOGIN_TIMEOUT_SECONDS}s for ${VM_SSH_USER}@${VM_IP}"
}

validate_guest_runtime() {
  if ! is_true "${VM_GUEST_CHECKS}"; then
    log "skipping guest runtime checks (CHAMICORE_VM_GUEST_CHECKS=${VM_GUEST_CHECKS})"
    return 0
  fi

  resolve_vm_ip
  check_console_login_prompt
  wait_for_ssh_port

  wait_for_ssh_login

  log "verifying cloud-init completion inside guest"
  local cloud_init_output
  cloud_init_output="$(run_vm_ssh 'if command -v cloud-init >/dev/null 2>&1; then cloud-init status --wait && echo cloud_init_status=done; elif [ -f /var/lib/cloud/instance/boot-finished ]; then echo cloud_init_status=done-file; else echo cloud_init_status=unknown; exit 1; fi')"

  if ! printf '%s\n' "${cloud_init_output}" | grep -Eq 'cloud_init_status=done|cloud_init_status=done-file'; then
    printf '%s\n' "${cloud_init_output}" >&2
    fail "cloud-init completion check failed in guest"
  fi

  log "guest runtime validation complete (login + cloud-init + ssh)"
}

main() {
  require_cmd curl
  require_cmd make
  require_cmd virsh
  require_cmd od
  require_cmd jq
  require_cmd nc
  require_cmd ssh
  require_cmd sshpass
  require_cmd timeout
  require_cmd script
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
  exercise_group_workflow
  exercise_bootparam_workflow
  validate_boot_path
  boot_vm
  validate_guest_runtime

  log "success"
  log "node_id=${NODE_ID}"
  log "mac=${MAC}"
  log "group_name=${GROUP_NAME}"
  log "bootparam_id=${BOOTPARAM_ID}"
  log "vm_name=${VM_NAME}"
  if [[ -n "${VM_IP}" ]]; then
    log "vm_ip=${VM_IP}"
  fi
}

main "$@"
