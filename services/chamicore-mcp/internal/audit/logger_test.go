package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestLoggerComplete_EmitsOneStructuredEntry(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	auditLogger := NewLogger(logger)

	auditLogger.Complete(ToolCallCompletion{
		RequestID: "req-1",
		SessionID: "sess-1",
		Transport: "http",
		ToolName:  "power.transitions.create",
		Mode:      "read-write",
		CallerSub: "agent-user",
		Arguments: map[string]any{
			"nodes":   []any{"x0", "x1"},
			"groups":  []any{"compute"},
			"token":   "super-secret",
			"confirm": true,
		},
		Result:       "success",
		Duration:     250 * time.Millisecond,
		ResponseCode: 200,
	})

	lines := splitJSONLines(t, buf.String())
	require.Len(t, lines, 1)

	entry := lines[0]
	require.Equal(t, "mcp.tool_call.completed", entry["event"])
	require.Equal(t, "req-1", entry["request_id"])
	require.Equal(t, "sess-1", entry["session_id"])
	require.Equal(t, "http", entry["transport"])
	require.Equal(t, "power.transitions.create", entry["tool"])
	require.Equal(t, "read-write", entry["mode"])
	require.Equal(t, "agent-user", entry["caller_subject"])
	require.Equal(t, "success", entry["result"])
	require.EqualValues(t, 250, entry["duration_ms"])

	target, ok := entry["target"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"x0", "x1"}, target["node_ids"])
	require.Equal(t, []any{"compute"}, target["group_ids"])
	_, hasToken := target["token"]
	require.False(t, hasToken)
}

func TestRedactSensitiveText_RedactsTokenLikeSegments(t *testing.T) {
	raw := "request failed: Authorization: Bearer abc.def.ghi token=xyz123 password=hunter2"
	redacted := RedactSensitiveText(raw)

	require.NotContains(t, redacted, "abc.def.ghi")
	require.NotContains(t, redacted, "xyz123")
	require.NotContains(t, redacted, "hunter2")
	require.Contains(t, redacted, "Authorization: [REDACTED]")
	require.Contains(t, redacted, "token=[REDACTED]")
	require.Contains(t, redacted, "password=[REDACTED]")
}

func TestSummarizeTargets_CollectsKnownIdentifiers(t *testing.T) {
	summary := SummarizeTargets(map[string]any{
		"id":           "scan-1",
		"component_id": "x1000c0s1b0n0",
		"groups":       []any{"compute", "gpu"},
		"nodes":        []any{"x1000c0s1b0n0", "x1000c0s1b0n1"},
		"members":      []any{"x1000c0s1b0n1", "x1000c0s1b0n2"},
	})

	require.Equal(t, []string{"x1000c0s1b0n0", "x1000c0s1b0n1"}, summary.NodeIDs)
	require.Equal(t, []string{"compute", "gpu"}, summary.GroupIDs)
	require.Equal(t, []string{"scan-1", "x1000c0s1b0n2"}, summary.ResourceIDs)
}

func splitJSONLines(t *testing.T, payload string) []map[string]any {
	t.Helper()

	rawLines := bytes.Split(bytes.TrimSpace([]byte(payload)), []byte("\n"))
	lines := make([]map[string]any, 0, len(rawLines))
	for _, raw := range rawLines {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var item map[string]any
		require.NoError(t, json.Unmarshal(raw, &item))
		lines = append(lines, item)
	}
	return lines
}
