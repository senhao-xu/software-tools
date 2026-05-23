package kube

import "testing"

func TestCRISocket(t *testing.T) {
	tests := []struct {
		name    string
		runtime string
		want    string
	}{
		{"docker", "docker", criDockerdSocket},
		{"containerd", "containerd", containerdSocket},
		{"empty defaults to containerd", "", containerdSocket},
		{"unknown defaults to containerd", "podman", containerdSocket},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := criSocket(tc.runtime); got != tc.want {
				t.Errorf("criSocket(%q) = %q, want %q", tc.runtime, got, tc.want)
			}
		})
	}
}
