package proxy

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	grpcFrameCountHeader       = "X-Apix-Grpc-Frame-Count"
	grpcCompressedCountHeader  = "X-Apix-Grpc-Compressed-Frames"
	grpcFrameLengthsHeader     = "X-Apix-Grpc-Frame-Lengths"
	grpcFrameParseErrorHeader  = "X-Apix-Grpc-Frame-Parse-Error"
	maxStoredFrameLengths      = 16
	grpcStatusHeader           = "Trailer-Grpc-Status"
	grpcMessageHeader          = "Trailer-Grpc-Message"
	grpcStatusDetailsBinHeader = "Trailer-Grpc-Status-Details-Bin"
)

func isGRPCMessage(headers http.Header) bool {
	ct := strings.ToLower(strings.TrimSpace(headers.Get("Content-Type")))
	return strings.HasPrefix(ct, "application/grpc")
}

func annotateGRPCFrames(headers http.Header, body []byte) {
	if !isGRPCMessage(headers) {
		return
	}
	summary := summarizeGRPCFrames(body)
	headers.Set(grpcFrameCountHeader, strconv.Itoa(summary.count))
	headers.Set(grpcCompressedCountHeader, strconv.Itoa(summary.compressedCount))
	if len(summary.lengths) > 0 {
		values := make([]string, 0, len(summary.lengths))
		for _, l := range summary.lengths {
			values = append(values, strconv.Itoa(l))
		}
		headers.Set(grpcFrameLengthsHeader, strings.Join(values, ","))
	}
	if summary.parseErr != "" {
		headers.Set(grpcFrameParseErrorHeader, summary.parseErr)
	}
}

func mergeTrailersIntoHeaders(headers, trailers http.Header) {
	for k, vv := range trailers {
		key := "Trailer-" + k
		for _, v := range vv {
			headers.Add(key, v)
		}
	}
}

func setGRPCStatusFromTrailers(headers http.Header) {
	if !isGRPCMessage(headers) {
		return
	}
	if headers.Get(grpcStatusHeader) == "" {
		headers.Set(grpcStatusHeader, "missing")
	}
	if headers.Get(grpcMessageHeader) == "" {
		headers.Set(grpcMessageHeader, "")
	}
	if headers.Get(grpcStatusDetailsBinHeader) == "" {
		headers.Set(grpcStatusDetailsBinHeader, "")
	}
}

type grpcFrameSummary struct {
	count           int
	compressedCount int
	lengths         []int
	parseErr        string
}

func summarizeGRPCFrames(body []byte) grpcFrameSummary {
	if len(body) == 0 {
		return grpcFrameSummary{}
	}

	out := grpcFrameSummary{
		lengths: make([]int, 0, 4),
	}
	offset := 0
	for offset < len(body) {
		if len(body)-offset < 5 {
			out.parseErr = fmt.Sprintf("truncated frame header at byte %d", offset)
			return out
		}
		flag := body[offset]
		if flag != 0 && flag != 1 {
			out.parseErr = fmt.Sprintf("invalid compression flag %d at byte %d", flag, offset)
			return out
		}
		length := int(binary.BigEndian.Uint32(body[offset+1 : offset+5]))
		offset += 5
		if len(body)-offset < length {
			out.parseErr = fmt.Sprintf("truncated frame payload at byte %d", offset)
			return out
		}
		out.count++
		if flag == 1 {
			out.compressedCount++
		}
		if len(out.lengths) < maxStoredFrameLengths {
			out.lengths = append(out.lengths, length)
		}
		offset += length
	}
	return out
}
