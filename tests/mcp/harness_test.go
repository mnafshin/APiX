package mcp_test

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSampleTranscriptExistsAndParses(t *testing.T) {
	f := "tests/mcp/fixtures/sample_transcript.json"
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var js map[string]interface{}
	if err := json.Unmarshal(b, &js); err != nil {
		t.Fatalf("invalid json fixture: %v", err)
	}
	if _, ok := js["transcript"]; !ok {
		t.Fatalf("expected 'transcript' key in fixture")
	}
}
