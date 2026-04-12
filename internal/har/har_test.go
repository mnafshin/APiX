package har

import (
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/storage"
)

func TestMarshalTransactions_IncludesBodiesAndHeaders(t *testing.T) {
	t.Parallel()

	reqs := []*storage.RequestRecord{{
		ID:         "req-1",
		Method:     "POST",
		URL:        "https://example.com/users?role=admin",
		Headers:    map[string]string{"Content-Type": "application/json", "X-Test": "1"},
		Body:       []byte(`{"name":"alice"}`),
		Timestamp:  time.Unix(1700000000, 0).UTC(),
		DurationMs: 142,
	}}
	resps := []*storage.ResponseRecord{{
		RequestID:  "req-1",
		StatusCode: 201,
		StatusText: "Created",
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"id":1}`),
	}}

	harJSON, err := MarshalTransactions(reqs, resps)
	if err != nil {
		t.Fatalf("MarshalTransactions: %v", err)
	}

	for _, want := range []string{
		`"version": "1.2"`,
		`"url": "https://example.com/users?role=admin"`,
		`"method": "POST"`,
		`"status": 201`,
		`"text": "{\"name\":\"alice\"}"`,
		`"text": "{\"id\":1}"`,
		`"name": "role"`,
		`"value": "admin"`,
	} {
		if !strings.Contains(harJSON, want) {
			t.Fatalf("expected HAR JSON to contain %q\n%s", want, harJSON)
		}
	}
}

func TestParseTransactions_RoundTripsBinaryAndText(t *testing.T) {
	t.Parallel()

	harJSON := `{
	  "log": {
	    "version": "1.2",
	    "creator": { "name": "tester", "version": "1" },
	    "entries": [{
	      "startedDateTime": "2024-01-01T00:00:00Z",
	      "time": 25,
	      "request": {
	        "method": "POST",
	        "url": "https://example.com/upload",
	        "httpVersion": "HTTP/1.1",
	        "headers": [{ "name": "Authorization", "value": "Bearer token" }],
	        "postData": {
	          "mimeType": "application/octet-stream",
	          "text": "AAE=",
	          "encoding": "base64"
	        },
	        "headersSize": -1,
	        "bodySize": 2
	      },
	      "response": {
	        "status": 200,
	        "statusText": "OK",
	        "httpVersion": "HTTP/1.1",
	        "headers": [],
	        "content": {
	          "mimeType": "application/json",
	          "text": "{\"ok\":true}"
	        },
	        "redirectURL": "",
	        "headersSize": -1,
	        "bodySize": 11
	      },
	      "timings": { "send": 0, "wait": 25, "receive": 0 }
	    }]
	  }
	}`

	imported, err := ParseTransactions(harJSON)
	if err != nil {
		t.Fatalf("ParseTransactions: %v", err)
	}
	if len(imported) != 1 {
		t.Fatalf("expected 1 imported transaction, got %d", len(imported))
	}

	tx := imported[0]
	if tx.Request == nil {
		t.Fatal("expected request")
	}
	if tx.Request.Method != "POST" {
		t.Fatalf("request method: got %q want POST", tx.Request.Method)
	}
	if string(tx.Request.Body) != string([]byte{0x00, 0x01}) {
		t.Fatalf("request body: got %v", tx.Request.Body)
	}
	if tx.Request.Headers["Content-Type"] != "application/octet-stream" {
		t.Fatalf("expected imported request content type, got %q", tx.Request.Headers["Content-Type"])
	}
	if tx.Response == nil {
		t.Fatal("expected response")
	}
	if tx.Response.StatusCode != 200 {
		t.Fatalf("response status: got %d want 200", tx.Response.StatusCode)
	}
	if string(tx.Response.Body) != `{"ok":true}` {
		t.Fatalf("response body: got %q", tx.Response.Body)
	}
	if tx.Response.Headers["Content-Type"] != "application/json" {
		t.Fatalf("expected imported response content type, got %q", tx.Response.Headers["Content-Type"])
	}
}
