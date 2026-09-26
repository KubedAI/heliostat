package domain

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Obj is an untyped JSON object, as decoded from Kubernetes or Ray responses. Custom resources are
// read without generated types so Heliostat tolerates KubeRay versions that add or rename fields.
type Obj = map[string]any

// Dict returns value as an object, or an empty one.
func Dict(value any) Obj {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return Obj{}
}

// Dig walks nested objects, returning an empty object for any missing step.
func Dig(value any, path ...string) Obj {
	current := Dict(value)
	for _, key := range path {
		current = Dict(current[key])
	}
	return current
}

// Str returns value as a string, or "".
func Str(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// List returns value as a list of objects.
func List(value any) []Obj {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]Obj, 0, len(items))
	for _, item := range items {
		out = append(out, Dict(item))
	}
	return out
}

// Strings returns the string elements of a list.
func Strings(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// OptNumber parses numbers that may arrive as JSON numbers or strings ("8265", "0.019").
func OptNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

// Number is OptNumber with a zero default.
func Number(value any) float64 {
	f, _ := OptNumber(value)
	return f
}

// Truncate shortens long text, marking the cut.
func Truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "\n… (truncated)"
}

// EpochMillisToISO converts Ray's millisecond timestamps; zero means unset.
func EpochMillisToISO(value any) string {
	ms, ok := OptNumber(value)
	if !ok || ms <= 0 {
		return ""
	}
	return time.UnixMilli(int64(ms)).UTC().Format("2006-01-02T15:04:05.000Z")
}
