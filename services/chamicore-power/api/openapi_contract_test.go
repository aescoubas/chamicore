package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestOpenAPIContract_ParsesAndHasRequiredPaths(t *testing.T) {
	doc := decodeOpenAPI(t)
	assert.Equal(t, "3.0.3", asString(doc["openapi"]))

	paths := mapAt(t, doc, "paths")
	for _, path := range []string{
		"/health",
		"/readiness",
		"/version",
		"/api/docs",
		"/api/openapi.yaml",
		"/power/v1/transitions",
		"/power/v1/transitions/{id}",
		"/power/v1/power-status",
		"/power/v1/actions/on",
		"/power/v1/actions/off",
		"/power/v1/actions/reboot",
		"/power/v1/actions/reset",
		"/power/v1/admin/mappings/sync",
	} {
		assert.Containsf(t, paths, path, "missing path %s", path)
	}
}

func TestOpenAPIContract_OperationEnumsMatchADR(t *testing.T) {
	doc := decodeOpenAPI(t)
	schemas := mapAt(t, mapAt(t, doc, "components"), "schemas")

	assert.ElementsMatch(
		t,
		[]string{"On", "ForceOff", "GracefulShutdown", "GracefulRestart", "ForceRestart", "Nmi"},
		stringSliceAt(t, mapAt(t, schemas, "PowerOperation"), "enum"),
	)

	assert.ElementsMatch(
		t,
		[]string{"pending", "running", "completed", "failed", "partial", "canceled", "planned"},
		stringSliceAt(t, mapAt(t, schemas, "TransitionState"), "enum"),
	)

	assert.ElementsMatch(
		t,
		[]string{"pending", "running", "succeeded", "failed", "canceled", "planned"},
		stringSliceAt(t, mapAt(t, schemas, "TaskState"), "enum"),
	)
}

func TestOpenAPIContract_ExamplesCoverRoadmapFlows(t *testing.T) {
	doc := decodeOpenAPI(t)
	examples := mapAt(t, mapAt(t, doc, "components"), "examples")

	for _, name := range []string{
		"SingleNodeTransitionRequest",
		"BulkTransitionRequest",
		"DryRunTransitionRequest",
		"TransitionAbortResponse",
	} {
		assert.Containsf(t, examples, name, "missing example %s", name)
	}

	paths := mapAt(t, doc, "paths")
	transitionsPost := operationAt(t, paths, "/power/v1/transitions", "post")
	requestExamples := mapAt(
		t,
		mapAt(
			t,
			mapAt(t, mapAt(t, transitionsPost, "requestBody"), "content"),
			"application/json",
		),
		"examples",
	)

	assert.Contains(t, requestExamples, "singleNode")
	assert.Contains(t, requestExamples, "bulk")
	assert.Contains(t, requestExamples, "dryRun")

	transitionDelete := operationAt(t, paths, "/power/v1/transitions/{id}", "delete")
	deleteExamples := mapAt(
		t,
		mapAt(
			t,
			mapAt(t, mapAt(t, mapAt(t, transitionDelete, "responses"), "202"), "content"),
			"application/json",
		),
		"examples",
	)
	assert.Contains(t, deleteExamples, "aborted")
}

func TestOpenAPIContract_ScopeAnnotationsPresent(t *testing.T) {
	doc := decodeOpenAPI(t)
	paths := mapAt(t, doc, "paths")

	type endpointMethod struct {
		Path   string
		Method string
	}

	expected := map[endpointMethod][]string{
		{Path: "/power/v1/transitions", Method: "get"}:          {"read:power", "admin"},
		{Path: "/power/v1/transitions", Method: "post"}:         {"write:power", "admin"},
		{Path: "/power/v1/transitions/{id}", Method: "get"}:     {"read:power", "admin"},
		{Path: "/power/v1/transitions/{id}", Method: "delete"}:  {"write:power", "admin"},
		{Path: "/power/v1/power-status", Method: "get"}:         {"read:power", "admin"},
		{Path: "/power/v1/actions/on", Method: "post"}:          {"write:power", "admin"},
		{Path: "/power/v1/actions/off", Method: "post"}:         {"write:power", "admin"},
		{Path: "/power/v1/actions/reboot", Method: "post"}:      {"write:power", "admin"},
		{Path: "/power/v1/actions/reset", Method: "post"}:       {"write:power", "admin"},
		{Path: "/power/v1/admin/mappings/sync", Method: "post"}: {"admin:power", "admin"},
	}

	for key, scopes := range expected {
		op := operationAt(t, paths, key.Path, key.Method)
		assert.ElementsMatch(t, scopes, stringSliceValue(t, op["x-required-scopes"]))
	}
}

func decodeOpenAPI(t *testing.T) map[string]any {
	t.Helper()

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(OpenAPISpec, &doc))
	require.NotEmpty(t, doc)
	return doc
}

func operationAt(t *testing.T, paths map[string]any, path, method string) map[string]any {
	t.Helper()
	pathItem := mapValue(t, paths[path], "paths["+path+"]")
	op, ok := pathItem[method]
	require.Truef(t, ok, "missing method %s on path %s", method, path)
	return mapValue(t, op, "paths["+path+"]["+method+"]")
}

func mapAt(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key]
	require.Truef(t, ok, "missing key %q", key)
	return mapValue(t, value, key)
}

func mapValue(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	out, ok := value.(map[string]any)
	require.Truef(t, ok, "%s must be an object", name)
	return out
}

func stringSliceAt(t *testing.T, parent map[string]any, key string) []string {
	t.Helper()
	value, ok := parent[key]
	require.Truef(t, ok, "missing key %q", key)
	return stringSliceValue(t, value)
}

func stringSliceValue(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	require.True(t, ok, "value must be an array")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, asString(item))
	}
	return out
}

func asString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
