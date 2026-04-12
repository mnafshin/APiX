package main

import (
"bytes"
"encoding/json"
"strings"
"testing"
)

func TestStatusJSON(t *testing.T) {
var out, errb bytes.Buffer
exit := Run([]string{"--output", "json", "status"}, &out, &errb)
if exit != 0 {
t.Fatalf("expected exit 0, got %d, err: %s", exit, errb.String())
}
var m map[string]interface{}
if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m); err != nil {
t.Fatalf("invalid json: %v", err)
}
if _, ok := m["status"]; !ok {
t.Fatalf("missing 'status' field in JSON output")
}
}

func TestWatchNDJSON(t *testing.T) {
var out, errb bytes.Buffer
exit := Run([]string{"--output", "ndjson", "watch", "--count", "2"}, &out, &errb)
if exit != 0 {
t.Fatalf("expected exit 0, got %d, err: %s", exit, errb.String())
}
lines := strings.Split(strings.TrimSpace(out.String()), "\n")
if len(lines) != 2 {
t.Fatalf("expected 2 ndjson lines, got %d: %q", len(lines), lines)
}
for _, l := range lines {
var obj map[string]interface{}
if err := json.Unmarshal([]byte(l), &obj); err != nil {
t.Fatalf("line not json: %v", err)
}
if _, ok := obj["event"]; !ok {
t.Fatalf("ndjson event missing 'event' field: %v", obj)
}
}
}

func TestUnknownCommandExit(t *testing.T) {
var out, errb bytes.Buffer
exit := Run([]string{"no-such-cmd"}, &out, &errb)
if exit == 0 {
t.Fatalf("expected non-zero exit for unknown command")
}
}
