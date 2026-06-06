package detect

import (
	"reflect"
	"testing"

	"xsh/internal/cri"
)

func TestCleanupResetSockets(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  []string
	}{
		{
			name:  "docker only",
			state: State{DockerActive: true},
			want:  []string{cri.DockerdSocket},
		},
		{
			name:  "docker command and containerd active",
			state: State{HasDockerCmd: true, ContainerdActive: true},
			want:  []string{cri.DockerdSocket, cri.ContainerdSocket},
		},
		{
			name:  "containerd only",
			state: State{ContainerdActive: true},
			want:  []string{cri.ContainerdSocket},
		},
		{
			name:  "unknown runtime falls back to containerd",
			state: State{},
			want:  []string{cri.ContainerdSocket},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanupResetSockets(tc.state); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("cleanupResetSockets() = %v, want %v", got, tc.want)
			}
		})
	}
}
