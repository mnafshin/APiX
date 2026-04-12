package mcp_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func isIgnoredKey(k string) bool {
	switch k {
	case "id", "request_id", "timestamp", "ts", "time":
		return true
	default:
		return false
	}
}

func normalize(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}
		for k, val := range t {
			if isIgnoredKey(k) {
				continue
			}
			out[k] = normalize(val)
		}
		return out
	case []interface{}:
		arr := make([]interface{}, len(t))
		for i, e := range t {
			arr[i] = normalize(e)
		}
		return arr
	default:
		return t
	}
}

func TestTranscriptNormalizedMatchesGolden(t *testing.T) {
	rawA, err := os.ReadFile("fixtures/sample_transcript.json")
	if err != nil {
		t.Fatalf("read fixture A: %v", err)
	}
	var a interface{}
	if err := json.Unmarshal(rawA, &a); err != nil {
		t.Fatalf("invalid json fixture A: %v", err)
	}
	normA := normalize(a)

	rawB, err := os.ReadFile("fixtures/sample_transcript.normalized.json")
	if err != nil {
		t.Fatalf("read golden fixture B: %v", err)
	}
	var b interface{}
	if err := json.Unmarshal(rawB, &b); err != nil {
		t.Fatalf("invalid json golden B: %v", err)
	}

	if !reflect.DeepEqual(normA, b) {
		aBytes, _ := json.MarshalIndent(normA, "", "  ")
		bBytes, _ := json.MarshalIndent(b, "", "  ")
		t.Fatalf("normalized transcript differs\nexpected:\n%s\nactual:\n%s", string(bBytes), string(aBytes))
	}
}
