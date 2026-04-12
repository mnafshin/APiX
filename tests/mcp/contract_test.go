// Package mcp_test provides schema-driven contract tests for the APiX MCP
// (Model Context Protocol) interface. These tests validate that the advertised
// tool/resource/prompt manifests and representative agent transcripts conform
// to the expected shapes, catching protocol drift before releases.
//
// Tests are purely data-driven and fast: no running engine is required.
package mcp_test

import (
	"encoding/json"
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// Schema helpers
// ---------------------------------------------------------------------------

// requireJSON reads a fixture file and unmarshals it into the target value.
func requireJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

// assertField fails the test if key is not present in m.
func assertField(t *testing.T, fixture string, m map[string]interface{}, key string) {
	t.Helper()
	if _, ok := m[key]; !ok {
		t.Errorf("%s: expected field %q to be present", fixture, key)
	}
}

// ---------------------------------------------------------------------------
// 1. Tools manifest: required fields and input schemas
// ---------------------------------------------------------------------------

func TestMCP_ToolsManifest_RequiredFields(t *testing.T) {
	var manifest struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	requireJSON(t, "fixtures/tools_manifest.json", &manifest)

	if len(manifest.Tools) == 0 {
		t.Fatal("tools_manifest.json: expected at least one tool")
	}

	for _, tool := range manifest.Tools {
		name, _ := tool["name"].(string)
		if name == "" {
			t.Error("tool entry missing or empty 'name'")
		}

		assertField(t, "tools_manifest.json["+name+"]", tool, "description")
		assertField(t, "tools_manifest.json["+name+"]", tool, "inputSchema")

		schema, ok := tool["inputSchema"].(map[string]interface{})
		if !ok {
			t.Errorf("tool %q: inputSchema must be an object", name)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q: inputSchema.type must be 'object', got %q", name, schema["type"])
		}
		if _, ok := schema["properties"]; !ok {
			t.Errorf("tool %q: inputSchema missing 'properties'", name)
		}
	}
}

// Verify the set of advertised tool names is stable — any removal is a
// breaking change that must be caught.
func TestMCP_ToolsManifest_AdvertisedNames(t *testing.T) {
	required := []string{
		"capture_traffic",
		"get_history",
		"set_breakpoint",
		"resume_request",
		"replay_request",
	}

	var manifest struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	requireJSON(t, "fixtures/tools_manifest.json", &manifest)

	found := make(map[string]bool, len(manifest.Tools))
	for _, t := range manifest.Tools {
		found[t.Name] = true
	}

	for _, name := range required {
		if !found[name] {
			t.Errorf("expected tool %q to be advertised in tools manifest", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Tools manifest: inputSchema property-level validation
// ---------------------------------------------------------------------------

func TestMCP_ToolsManifest_SetBreakpoint_RequiredProperty(t *testing.T) {
	var manifest struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Required   []string               `json:"required"`
				Properties map[string]interface{} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	requireJSON(t, "fixtures/tools_manifest.json", &manifest)

	for _, tool := range manifest.Tools {
		if tool.Name != "set_breakpoint" {
			continue
		}
		// url_pattern is the only required arg for set_breakpoint.
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "url_pattern" {
				found = true
				break
			}
		}
		if !found {
			t.Error("set_breakpoint: 'url_pattern' must be in inputSchema.required")
		}

		// method property must declare a string enum.
		methodProp, ok := tool.InputSchema.Properties["method"].(map[string]interface{})
		if !ok {
			t.Error("set_breakpoint: 'method' property must be present")
			return
		}
		if methodProp["type"] != "string" {
			t.Errorf("set_breakpoint.method: expected type 'string', got %v", methodProp["type"])
		}
		if _, ok := methodProp["enum"]; !ok {
			t.Error("set_breakpoint.method: expected 'enum' constraint")
		}
		return
	}
	t.Error("set_breakpoint tool not found in manifest")
}

func TestMCP_ToolsManifest_ResumeRequest_ActionEnum(t *testing.T) {
	var manifest struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Required   []string               `json:"required"`
				Properties map[string]interface{} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	requireJSON(t, "fixtures/tools_manifest.json", &manifest)

	for _, tool := range manifest.Tools {
		if tool.Name != "resume_request" {
			continue
		}
		// Both request_id and action must be required.
		hasReqID, hasAction := false, false
		for _, r := range tool.InputSchema.Required {
			if r == "request_id" {
				hasReqID = true
			}
			if r == "action" {
				hasAction = true
			}
		}
		if !hasReqID {
			t.Error("resume_request: 'request_id' must be in inputSchema.required")
		}
		if !hasAction {
			t.Error("resume_request: 'action' must be in inputSchema.required")
		}

		// action must be an enum of exactly forward/drop/respond.
		actionProp, ok := tool.InputSchema.Properties["action"].(map[string]interface{})
		if !ok {
			t.Error("resume_request: 'action' property must be present")
			return
		}
		enum, ok := actionProp["enum"].([]interface{})
		if !ok {
			t.Error("resume_request.action: 'enum' must be a list")
			return
		}
		expected := map[string]bool{"forward": true, "drop": true, "respond": true}
		for _, v := range enum {
			s, ok := v.(string)
			if !ok || !expected[s] {
				t.Errorf("resume_request.action: unexpected enum value %v", v)
			}
		}
		if len(enum) != len(expected) {
			t.Errorf("resume_request.action: expected %d enum values, got %d", len(expected), len(enum))
		}
		return
	}
	t.Error("resume_request tool not found in manifest")
}

// ---------------------------------------------------------------------------
// 3. Resources and prompts manifest
// ---------------------------------------------------------------------------

func TestMCP_ResourcesManifest_RequiredFields(t *testing.T) {
	var manifest struct {
		Resources []map[string]interface{} `json:"resources"`
	}
	requireJSON(t, "fixtures/resources_prompts_manifest.json", &manifest)

	if len(manifest.Resources) == 0 {
		t.Fatal("resources_prompts_manifest.json: expected at least one resource")
	}

	requiredURIs := []string{"apix://history", "apix://breakpoints", "apix://plugins"}
	found := make(map[string]bool)

	for _, res := range manifest.Resources {
		uri, _ := res["uri"].(string)
		found[uri] = true
		assertField(t, "resource["+uri+"]", res, "name")
		assertField(t, "resource["+uri+"]", res, "description")
		assertField(t, "resource["+uri+"]", res, "mimeType")
	}

	for _, uri := range requiredURIs {
		if !found[uri] {
			t.Errorf("expected resource URI %q to be advertised", uri)
		}
	}
}

func TestMCP_PromptsManifest_RequiredFields(t *testing.T) {
	var manifest struct {
		Prompts []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Arguments   []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"arguments"`
		} `json:"prompts"`
	}
	requireJSON(t, "fixtures/resources_prompts_manifest.json", &manifest)

	if len(manifest.Prompts) == 0 {
		t.Fatal("resources_prompts_manifest.json: expected at least one prompt")
	}

	for _, p := range manifest.Prompts {
		if p.Name == "" {
			t.Error("prompt entry missing 'name'")
		}
		if p.Description == "" {
			t.Errorf("prompt %q missing 'description'", p.Name)
		}
	}
}

func TestMCP_PromptsManifest_DebugAPI_HasRequiredArg(t *testing.T) {
	var manifest struct {
		Prompts []struct {
			Name      string `json:"name"`
			Arguments []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"arguments"`
		} `json:"prompts"`
	}
	requireJSON(t, "fixtures/resources_prompts_manifest.json", &manifest)

	for _, p := range manifest.Prompts {
		if p.Name != "debug_api" {
			continue
		}
		for _, arg := range p.Arguments {
			if arg.Name == "target_url" && arg.Required {
				return // found
			}
		}
		t.Error("debug_api prompt: 'target_url' argument must be required")
		return
	}
	t.Error("debug_api prompt not found in manifest")
}

// ---------------------------------------------------------------------------
// 4. Transcript golden tests
// ---------------------------------------------------------------------------

// transcriptEntry represents one step in an agent workflow transcript.
type transcriptEntry struct {
	Role    string                 `json:"role"`
	Type    string                 `json:"type"`
	Tool    string                 `json:"tool,omitempty"`
	Content map[string]interface{} `json:"content,omitempty"`
}

func loadTranscript(t *testing.T, path string) []transcriptEntry {
	t.Helper()
	var doc struct {
		Transcript []transcriptEntry `json:"transcript"`
		Metadata   map[string]string `json:"metadata"`
	}
	requireJSON(t, path, &doc)

	if len(doc.Transcript) == 0 {
		t.Fatalf("%s: transcript must have at least one entry", path)
	}
	if doc.Metadata["version"] == "" {
		t.Errorf("%s: metadata.version must be present", path)
	}
	return doc.Transcript
}

// assertTranscriptShape checks that every entry has required fields and that
// the role/type values are valid.
func assertTranscriptShape(t *testing.T, path string, entries []transcriptEntry) {
	t.Helper()
	validRoles := map[string]bool{"user": true, "assistant": true, "system": true}
	validTypes := map[string]bool{"tool_call": true, "tool_result": true, "message": true}

	for i, e := range entries {
		if !validRoles[e.Role] {
			t.Errorf("%s[%d]: invalid role %q", path, i, e.Role)
		}
		if !validTypes[e.Type] {
			t.Errorf("%s[%d]: invalid type %q", path, i, e.Type)
		}
		// tool_call and tool_result must name a tool.
		if (e.Type == "tool_call" || e.Type == "tool_result") && e.Tool == "" {
			t.Errorf("%s[%d]: type %q must have a 'tool' field", path, i, e.Type)
		}
		// assistant tool_result must include content with a 'status' field.
		if e.Role == "assistant" && e.Type == "tool_result" {
			if e.Content == nil {
				t.Errorf("%s[%d]: assistant tool_result must have 'content'", path, i)
			} else if _, ok := e.Content["status"]; !ok {
				t.Errorf("%s[%d]: assistant tool_result.content must have 'status'", path, i)
			}
		}
	}
}

// assertToolCallResultPairs checks that every user tool_call has a
// corresponding assistant tool_result with the same tool name immediately after.
func assertToolCallResultPairs(t *testing.T, path string, entries []transcriptEntry) {
	t.Helper()
	for i := 0; i < len(entries)-1; i++ {
		curr := entries[i]
		next := entries[i+1]
		if curr.Role == "user" && curr.Type == "tool_call" {
			if next.Role != "assistant" || next.Type != "tool_result" {
				t.Errorf("%s[%d]: tool_call by user must be followed by assistant tool_result", path, i)
				continue
			}
			if curr.Tool != next.Tool {
				t.Errorf("%s[%d]: tool_call for %q followed by tool_result for %q (mismatch)", path, i, curr.Tool, next.Tool)
			}
		}
	}
}

func TestMCP_Transcript_CaptureInspect_Shape(t *testing.T) {
	path := "fixtures/transcript_capture_inspect.json"
	entries := loadTranscript(t, path)
	assertTranscriptShape(t, path, entries)
	assertToolCallResultPairs(t, path, entries)
}

func TestMCP_Transcript_BreakpointRespond_Shape(t *testing.T) {
	path := "fixtures/transcript_breakpoint_respond.json"
	entries := loadTranscript(t, path)
	assertTranscriptShape(t, path, entries)
	assertToolCallResultPairs(t, path, entries)
}

func TestMCP_Transcript_ReplayOverride_Shape(t *testing.T) {
	path := "fixtures/transcript_replay_override.json"
	entries := loadTranscript(t, path)
	assertTranscriptShape(t, path, entries)
	assertToolCallResultPairs(t, path, entries)
}

func TestMCP_Transcript_ReplayOverride_ToolNameStable(t *testing.T) {
	entries := loadTranscript(t, "fixtures/transcript_replay_override.json")
	if len(entries) == 0 {
		t.Fatal("empty transcript")
	}
	first := entries[0]
	if first.Tool != "replay_request" {
		t.Errorf("expected first tool to be 'replay_request', got %q", first.Tool)
	}
}

func TestMCP_Transcript_BreakpointRespond_ActionForwarded(t *testing.T) {
	entries := loadTranscript(t, "fixtures/transcript_breakpoint_respond.json")

	// Find the resume_request result and verify action_taken is 'respond'.
	for _, e := range entries {
		if e.Role == "assistant" && e.Tool == "resume_request" && e.Content != nil {
			if e.Content["action_taken"] != "respond" {
				t.Errorf("expected action_taken='respond', got %v", e.Content["action_taken"])
			}
			statusCode, _ := e.Content["response_status"].(float64)
			if int(statusCode) != 200 {
				t.Errorf("expected response_status=200, got %v", e.Content["response_status"])
			}
			return
		}
	}
	t.Error("resume_request tool_result not found in transcript")
}

// ---------------------------------------------------------------------------
// 5. Error response shape validation
// ---------------------------------------------------------------------------

// mcpError mirrors the JSON-RPC 2.0 error object shape used by MCP.
type mcpError struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

type mcpErrorEnvelope struct {
	Error mcpError `json:"error"`
}

func TestMCP_ErrorShape_InvalidParams(t *testing.T) {
	var env mcpErrorEnvelope
	requireJSON(t, "fixtures/error_invalid_params.json", &env)

	// JSON-RPC 2.0 error code for invalid params.
	if env.Error.Code != -32602 {
		t.Errorf("expected error code -32602 (Invalid params), got %d", env.Error.Code)
	}
	if env.Error.Message == "" {
		t.Error("error.message must not be empty")
	}
	if env.Error.Data == nil {
		t.Error("error.data should be present for invalid params error")
	}
}

func TestMCP_ErrorShape_MethodNotFound(t *testing.T) {
	var env mcpErrorEnvelope
	requireJSON(t, "fixtures/error_method_not_found.json", &env)

	if env.Error.Code != -32601 {
		t.Errorf("expected error code -32601 (Method not found), got %d", env.Error.Code)
	}
	if env.Error.Message == "" {
		t.Error("error.message must not be empty")
	}
	// data.requested_tool should name the unknown tool.
	if env.Error.Data == nil || env.Error.Data["requested_tool"] == "" {
		t.Error("error.data.requested_tool should name the unrecognised tool")
	}
}

// ---------------------------------------------------------------------------
// 6. Backward-compatibility: tool names and error codes must not change
// ---------------------------------------------------------------------------

// TestMCP_BackwardCompat_ErrorCodes ensures the JSON-RPC error codes haven't
// drifted from their spec values. These are load-bearing for any tool that
// inspects error codes programmatically.
func TestMCP_BackwardCompat_ErrorCodes(t *testing.T) {
	cases := []struct {
		fixture      string
		expectedCode int
	}{
		{"fixtures/error_invalid_params.json", -32602},
		{"fixtures/error_method_not_found.json", -32601},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			var env mcpErrorEnvelope
			requireJSON(t, tc.fixture, &env)
			if env.Error.Code != tc.expectedCode {
				t.Errorf("expected code %d, got %d", tc.expectedCode, env.Error.Code)
			}
		})
	}
}

// TestMCP_BackwardCompat_ToolNames verifies that tool names in the manifest
// haven't silently changed (renames break existing agents).
func TestMCP_BackwardCompat_ToolNames(t *testing.T) {
	stable := map[string]bool{
		"capture_traffic": true,
		"get_history":     true,
		"set_breakpoint":  true,
		"resume_request":  true,
		"replay_request":  true,
	}

	var manifest struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	requireJSON(t, "fixtures/tools_manifest.json", &manifest)

	for _, tool := range manifest.Tools {
		if !stable[tool.Name] {
			t.Errorf("tool %q is not in the stable tool name set — is this a rename?", tool.Name)
		}
	}

	for name := range stable {
		found := false
		for _, tool := range manifest.Tools {
			if tool.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("stable tool %q is missing from the manifest — breaking change", name)
		}
	}
}
