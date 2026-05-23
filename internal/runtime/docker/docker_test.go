package dockerrt

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRenderDaemonJSON(t *testing.T) {
	body, err := renderDaemonJSON()
	if err != nil {
		t.Fatalf("renderDaemonJSON() error = %v", err)
	}

	// Must be valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("renderDaemonJSON() produced invalid JSON: %v\nbody:\n%s", err, body)
	}

	// log-driver
	if got, want := parsed["log-driver"], "json-file"; got != want {
		t.Errorf("log-driver = %v, want %v", got, want)
	}

	// log-opts: max-file=3, max-size=100m
	logOpts, ok := parsed["log-opts"].(map[string]any)
	if !ok {
		t.Fatalf("log-opts not a map: %T", parsed["log-opts"])
	}
	if got, want := logOpts["max-file"], "3"; got != want {
		t.Errorf("log-opts.max-file = %v, want %v", got, want)
	}
	if got, want := logOpts["max-size"], "100m"; got != want {
		t.Errorf("log-opts.max-size = %v, want %v", got, want)
	}

	// exec-opts includes native.cgroupdriver=systemd
	execOpts, ok := parsed["exec-opts"].([]any)
	if !ok {
		t.Fatalf("exec-opts not a slice: %T", parsed["exec-opts"])
	}
	want := []any{"native.cgroupdriver=systemd"}
	if !reflect.DeepEqual(execOpts, want) {
		t.Errorf("exec-opts = %v, want %v", execOpts, want)
	}

	// Trailing newline (POSIX convention).
	if body[len(body)-1] != '\n' {
		t.Errorf("renderDaemonJSON() should end with newline; last byte = %q", body[len(body)-1])
	}
}
