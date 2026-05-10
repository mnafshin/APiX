package learn

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type PathParameter struct {
	Name   string
	Type   string
	Format string
}

// NormalizePath converts concrete URL paths into OpenAPI path templates.
func NormalizePath(rawURL string) (string, []PathParameter) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "/", nil
	}

	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return "/", nil
	}

	params := make([]PathParameter, 0, len(parts))
	seenNames := map[string]int{}

	for i := range parts {
		segment, unescapeErr := url.PathUnescape(parts[i])
		if unescapeErr != nil {
			segment = parts[i]
		}
		if isNumericSegment(segment) {
			name := nextParamName("id", seenNames)
			parts[i] = "{" + name + "}"
			params = append(params, PathParameter{Name: name, Type: "integer"})
			continue
		}
		if uuidRE.MatchString(segment) {
			name := nextParamName("id", seenNames)
			parts[i] = "{" + name + "}"
			params = append(params, PathParameter{Name: name, Type: "string", Format: "uuid"})
			continue
		}
		parts[i] = segment
	}

	return "/" + strings.Join(parts, "/"), params
}

func isNumericSegment(segment string) bool {
	if segment == "" {
		return false
	}
	_, err := strconv.ParseInt(segment, 10, 64)
	return err == nil
}

func nextParamName(base string, seen map[string]int) string {
	seen[base]++
	if seen[base] == 1 {
		return base
	}
	return base + strconv.Itoa(seen[base])
}
