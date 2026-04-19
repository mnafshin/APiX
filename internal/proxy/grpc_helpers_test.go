package proxy

import (
	"encoding/binary"
	"net/http"
	"testing"
)

func grpcFrame(flag byte, payload []byte) []byte {
	out := make([]byte, 5+len(payload))
	out[0] = flag
	binary.BigEndian.PutUint32(out[1:5], uint32(len(payload)))
	copy(out[5:], payload)
	return out
}

func TestAnnotateGRPCFrames_ValidUnaryPayload(t *testing.T) {
	t.Parallel()

	headers := http.Header{"Content-Type": []string{"application/grpc+proto"}}
	body := grpcFrame(0, []byte("hello"))
	annotateGRPCFrames(headers, body)

	if got := headers.Get(grpcFrameCountHeader); got != "1" {
		t.Fatalf("frame count: got %q want %q", got, "1")
	}
	if got := headers.Get(grpcCompressedCountHeader); got != "0" {
		t.Fatalf("compressed frame count: got %q want %q", got, "0")
	}
	if got := headers.Get(grpcFrameLengthsHeader); got != "5" {
		t.Fatalf("frame lengths: got %q want %q", got, "5")
	}
	if got := headers.Get(grpcFrameParseErrorHeader); got != "" {
		t.Fatalf("parse error: got %q want empty", got)
	}
}

func TestAnnotateGRPCFrames_TruncatedPayloadSetsParseError(t *testing.T) {
	t.Parallel()

	headers := http.Header{"Content-Type": []string{"application/grpc"}}
	body := append(grpcFrame(0, []byte("abc"))[:6], byte('x'))
	annotateGRPCFrames(headers, body)

	if got := headers.Get(grpcFrameParseErrorHeader); got == "" {
		t.Fatal("expected parse error for truncated frame payload")
	}
}
