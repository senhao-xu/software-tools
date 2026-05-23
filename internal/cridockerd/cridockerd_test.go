package cridockerd

import (
	"strings"
	"testing"
)

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		distro   string
		codename string
		arch     string
		want     string
		wantErr  bool
	}{
		{
			name:     "debian bookworm amd64 native",
			version:  "0.3.21",
			distro:   "debian",
			codename: "bookworm",
			arch:     "amd64",
			want:     "https://github.com/Mirantis/cri-dockerd/releases/download/v0.3.21/cri-dockerd_0.3.21.3-0.debian-bookworm_amd64.deb",
		},
		{
			name:     "debian trixie falls back to bookworm",
			version:  "0.3.21",
			distro:   "debian",
			codename: "trixie",
			arch:     "amd64",
			want:     "https://github.com/Mirantis/cri-dockerd/releases/download/v0.3.21/cri-dockerd_0.3.21.3-0.debian-bookworm_amd64.deb",
		},
		{
			name:     "ubuntu jammy amd64 native",
			version:  "0.3.21",
			distro:   "ubuntu",
			codename: "jammy",
			arch:     "amd64",
			want:     "https://github.com/Mirantis/cri-dockerd/releases/download/v0.3.21/cri-dockerd_0.3.21.3-0.ubuntu-jammy_amd64.deb",
		},
		{
			name:     "ubuntu noble falls back to jammy",
			version:  "0.3.21",
			distro:   "ubuntu",
			codename: "noble",
			arch:     "amd64",
			want:     "https://github.com/Mirantis/cri-dockerd/releases/download/v0.3.21/cri-dockerd_0.3.21.3-0.ubuntu-jammy_amd64.deb",
		},
		{
			name:     "debian bookworm arm64",
			version:  "0.3.21",
			distro:   "debian",
			codename: "bookworm",
			arch:     "arm64",
			want:     "https://github.com/Mirantis/cri-dockerd/releases/download/v0.3.21/cri-dockerd_0.3.21.3-0.debian-bookworm_arm64.deb",
		},
		{
			name:    "empty version rejected",
			version: "",
			distro:  "debian", codename: "bookworm", arch: "amd64",
			wantErr: true,
		},
		{
			name:    "centos rejected",
			version: "0.3.21",
			distro:  "centos", codename: "8", arch: "amd64",
			wantErr: true,
		},
		{
			name:    "debian bullseye not supported (oldstable, outside our matrix)",
			version: "0.3.21",
			distro:  "debian", codename: "bullseye", arch: "amd64",
			wantErr: true,
		},
		{
			name:    "ubuntu focal not supported (outside our matrix)",
			version: "0.3.21",
			distro:  "ubuntu", codename: "focal", arch: "amd64",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildURL(tc.version, tc.distro, tc.codename, tc.arch)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("BuildURL() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildURL() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("BuildURL() = %q, want %q", got, tc.want)
			}
			if !strings.HasPrefix(got, "https://github.com/Mirantis/cri-dockerd/releases/download/v") {
				t.Errorf("BuildURL() = %q, want GitHub releases URL", got)
			}
		})
	}
}
