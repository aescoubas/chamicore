# MCP Operator Guide

This guide shows how to run and use `chamicore-mcp` in both supported transport modes:
- local `stdio` (agent-launched process)
- deployed `HTTP/SSE` (Compose/Helm service)

Assumptions:
- Chamicore stack is running (`make compose-up` for local Compose).
- You are at repo root.
- `jq` is installed for readable output.

## 1. Endpoints and Auth Model

Default local endpoints:
- gateway HTTP/SSE: `http://localhost:8080/mcp/v1`
- direct service HTTP/SSE: `http://localhost:27774/mcp/v1`
- tools contract: `http://localhost:8080/mcp/api/tools.yaml`

HTTP/SSE calls require:
- `Authorization: Bearer <session-token>`
- `Content-Type: application/json`

Token resolution precedence in the MCP service:
1. `CHAMICORE_MCP_TOKEN`
2. `CHAMICORE_TOKEN`
3. `~/.chamicore/config.yaml` `auth.token` only when `CHAMICORE_MCP_ALLOW_CLI_CONFIG_TOKEN=true`

Safe mode defaults:
- `CHAMICORE_MCP_MODE=read-only`
- `CHAMICORE_MCP_ENABLE_WRITE=false`

## 2. Local Stdio Setup (Agent-Local Process)

Build the binary:

```bash
mkdir -p bin
(cd services/chamicore-mcp && go build -o ../../bin/chamicore-mcp ./cmd/chamicore-mcp)
```

Configure stdio transport against local stack:

```bash
export CHAMICORE_MCP_TRANSPORT=stdio
export CHAMICORE_MCP_MODE=read-only
export CHAMICORE_MCP_ENABLE_WRITE=false
export CHAMICORE_MCP_TOKEN="${CHAMICORE_MCP_TOKEN:-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef}"
export CHAMICORE_MCP_AUTH_URL=http://localhost:3333
export CHAMICORE_MCP_SMD_URL=http://localhost:27779
export CHAMICORE_MCP_BSS_URL=http://localhost:27778
export CHAMICORE_MCP_CLOUD_INIT_URL=http://localhost:27777
export CHAMICORE_MCP_DISCOVERY_URL=http://localhost:27776
export CHAMICORE_MCP_POWER_URL=http://localhost:27775
```

Run a simple stdio MCP session (`initialize` -> `tools/list` -> read tool call):

```bash
cat <<'EOF' | ./bin/chamicore-mcp | jq -c .
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cluster.health_summary","arguments":{}}}
EOF
```

## 3. Deployed HTTP/SSE Setup (Compose and Helm)

Compose mode (default local deployment):

```bash
make compose-up
export MCP_BASE=http://localhost:8080/mcp/v1
export MCP_TOKEN="${CHAMICORE_MCP_TOKEN:-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef}"
curl -fsS http://localhost:8080/_ops/mcp/health | jq .
curl -fsS http://localhost:8080/_ops/mcp/readiness | jq .
```

Helm mode (cluster deployment):

```bash
helm upgrade --install chamicore shared/chamicore-deploy/charts/chamicore \
  --set mcp.enabled=true \
  --set mcp.transport=http \
  --set mcp.mode=read-only \
  --set mcp.enableWrite=false
```

Read workflow over HTTP:

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${MCP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"cluster.health_summary","arguments":{}}' \
  "${MCP_BASE}/tools/call" | jq .
```

Read workflow over SSE:

```bash
curl -N -sS -X POST \
  -H "Authorization: Bearer ${MCP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"cluster.health_summary","arguments":{}}' \
  "${MCP_BASE}/tools/call/sse"
```

## 4. Safe Transition: Read-Only to Read-Write

Verify write denial first (expected `403` in read-only mode):

```bash
curl -sS -D - -o /tmp/mcp-write-denied.json -X POST \
  -H "Authorization: Bearer ${MCP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"bss.bootparams.upsert","arguments":{"component_id":"node-mcp-demo","kernel":"http://172.16.10.1:8080/pxe/vmlinuz","initrd":"http://172.16.10.1:8080/pxe/initrd.img","params":["console=ttyS0","ip=dhcp"]}}' \
  "${MCP_BASE}/tools/call"
cat /tmp/mcp-write-denied.json | jq .
```

Temporarily enable write mode for Compose and restart only MCP:

```bash
CHAMICORE_MCP_MODE=read-write \
CHAMICORE_MCP_ENABLE_WRITE=true \
docker compose \
  -f shared/chamicore-deploy/docker-compose.yml \
  -f shared/chamicore-deploy/docker-compose.override.yml \
  up -d mcp
```

Run the same write call again (now expected `200`):

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${MCP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"bss.bootparams.upsert","arguments":{"component_id":"node-mcp-demo","kernel":"http://172.16.10.1:8080/pxe/vmlinuz","initrd":"http://172.16.10.1:8080/pxe/initrd.img","params":["console=ttyS0","ip=dhcp"]}}' \
  "${MCP_BASE}/tools/call" | jq .
```

Return to safe default after write operations:

```bash
CHAMICORE_MCP_MODE=read-only \
CHAMICORE_MCP_ENABLE_WRITE=false \
docker compose \
  -f shared/chamicore-deploy/docker-compose.yml \
  -f shared/chamicore-deploy/docker-compose.override.yml \
  up -d mcp
```

## 5. Destructive Confirmation Examples

Delete without `confirm=true` (expected `400`):

```bash
curl -sS -D - -o /tmp/mcp-delete-denied.json -X POST \
  -H "Authorization: Bearer ${MCP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"bss.bootparams.delete","arguments":{"component_id":"node-mcp-demo"}}' \
  "${MCP_BASE}/tools/call"
cat /tmp/mcp-delete-denied.json | jq .
```

Delete with explicit confirmation (expected success):

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${MCP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"bss.bootparams.delete","arguments":{"component_id":"node-mcp-demo","confirm":true}}' \
  "${MCP_BASE}/tools/call" | jq .
```

## 6. Troubleshooting

| Symptom | Likely Cause | Resolution |
|---|---|---|
| `401` + `MCP session token is not configured` | MCP has no resolved token | Set `CHAMICORE_MCP_TOKEN` or `CHAMICORE_TOKEN`; for stdio optional fallback requires `CHAMICORE_MCP_ALLOW_CLI_CONFIG_TOKEN=true`. |
| `401` + `missing or malformed Authorization header` | Missing `Authorization: Bearer ...` in HTTP/SSE call | Add bearer header to every HTTP/SSE tool call. |
| `401` + `invalid bearer token for MCP session` | Client token does not match MCP configured token | Use the exact `CHAMICORE_MCP_TOKEN` value configured in the running MCP service. |
| `403` + `requires read-write mode` | Write tool called while MCP is in read-only mode | Set both `CHAMICORE_MCP_MODE=read-write` and `CHAMICORE_MCP_ENABLE_WRITE=true`, then restart MCP. |
| `400` + `requires confirm=true` | Destructive tool called without confirmation | Add `"confirm": true` in tool arguments. |
| `403` + `missing required scopes` | JWT lacks required scope metadata | Use an admin-scope token for V1 tools or the internal token in trusted local/dev contexts. |

Inspect MCP logs for tool-level audit events:

```bash
docker compose \
  -f shared/chamicore-deploy/docker-compose.yml \
  -f shared/chamicore-deploy/docker-compose.override.yml \
  logs -f mcp
```
