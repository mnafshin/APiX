package replay

import (
	"net/http"
	"strings"

	"github.com/mnafshin/apix/internal/storage"
)

// ReplayDiff summarises the differences between a stored transaction's response
// and a freshly replayed response.
type ReplayDiff struct {
	StatusMatch      bool
	StatusOriginal   int
	StatusReplayed   int
	HeaderDiffs      []HeaderDiff
	BodyMatch        bool
	BodySizeOriginal int
	BodySizeReplayed int
}

// HeaderDiff describes a single header that differs between the two responses.
// A zero Original means the header was absent in the original; a zero Replayed
// means it was absent in the replay.
type HeaderDiff struct {
	Name     string
	Original string
	Replayed string
}

// DiffResponses compares the stored response in original against replayed.
// When original has no response record (nil) all fields report a mismatch.
func DiffResponses(original *storage.Transaction, replayed *http.Response) ReplayDiff {
	d := ReplayDiff{}

	if original == nil || original.Response == nil {
		// Nothing to compare against.
		if replayed != nil {
			d.StatusReplayed = replayed.StatusCode
			d.BodySizeReplayed = int(replayed.ContentLength)
		}
		return d
	}

	origResp := original.Response

	// --- Status comparison ---
	d.StatusOriginal = origResp.StatusCode
	d.StatusReplayed = replayed.StatusCode
	d.StatusMatch = origResp.StatusCode == replayed.StatusCode

	// --- Header comparison ---
	// Normalise to lowercase keys for case-insensitive comparison.
	origHeaders := normaliseHeaders(origResp.Headers)
	replayHeaders := make(map[string]string, len(replayed.Header))
	for k, vals := range replayed.Header {
		replayHeaders[strings.ToLower(k)] = strings.Join(vals, ", ")
	}

	seen := make(map[string]struct{})
	for k, ov := range origHeaders {
		seen[k] = struct{}{}
		rv := replayHeaders[k]
		if ov != rv {
			d.HeaderDiffs = append(d.HeaderDiffs, HeaderDiff{
				Name:     k,
				Original: ov,
				Replayed: rv,
			})
		}
	}
	for k, rv := range replayHeaders {
		if _, exists := seen[k]; !exists {
			d.HeaderDiffs = append(d.HeaderDiffs, HeaderDiff{
				Name:     k,
				Original: "",
				Replayed: rv,
			})
		}
	}

	// --- Body size comparison ---
	d.BodySizeOriginal = len(origResp.Body)
	if replayed.ContentLength >= 0 {
		d.BodySizeReplayed = int(replayed.ContentLength) //nolint:gosec // ContentLength is bounded
	}
	d.BodyMatch = d.BodySizeOriginal == d.BodySizeReplayed

	return d
}

func normaliseHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
