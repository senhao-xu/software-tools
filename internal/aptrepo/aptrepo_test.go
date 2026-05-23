package aptrepo

import (
	"strings"
	"testing"

	"xsh/internal/osinfo"
)

func TestDistroCodename(t *testing.T) {
	tests := []struct {
		name      string
		info      *osinfo.OSInfo
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "debian 12 bookworm",
			info: &osinfo.OSInfo{ID: "debian", VersionID: "12"},
			want: "bookworm",
		},
		{
			name: "debian 13 trixie",
			info: &osinfo.OSInfo{ID: "debian", VersionID: "13"},
			want: "trixie",
		},
		{
			name: "ubuntu 22.04 jammy",
			info: &osinfo.OSInfo{ID: "ubuntu", VersionID: "22.04"},
			want: "jammy",
		},
		{
			name: "ubuntu 24.04 noble",
			info: &osinfo.OSInfo{ID: "ubuntu", VersionID: "24.04"},
			want: "noble",
		},
		{
			name:      "nil info",
			info:      nil,
			wantErr:   true,
			errSubstr: "nil",
		},
		{
			name:      "centos rejected",
			info:      &osinfo.OSInfo{ID: "centos", VersionID: "8"},
			wantErr:   true,
			errSubstr: "unsupported distro",
		},
		{
			name:      "debian 11 rejected",
			info:      &osinfo.OSInfo{ID: "debian", VersionID: "11"},
			wantErr:   true,
			errSubstr: "unsupported debian version",
		},
		{
			name:      "ubuntu 20.04 rejected",
			info:      &osinfo.OSInfo{ID: "ubuntu", VersionID: "20.04"},
			wantErr:   true,
			errSubstr: "unsupported ubuntu version",
		},
		{
			name:      "linuxmint rejected (no ID_LIKE fallback)",
			info:      &osinfo.OSInfo{ID: "linuxmint", VersionID: "21"},
			wantErr:   true,
			errSubstr: "unsupported distro",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DistroCodename(tc.info)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DistroCodename() = %q, want error containing %q", got, tc.errSubstr)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("DistroCodename() error = %q, want substring %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DistroCodename() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("DistroCodename() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDockerURLPrefix(t *testing.T) {
	tests := []struct {
		name    string
		info    *osinfo.OSInfo
		want    string
		wantErr bool
	}{
		{name: "debian 12", info: &osinfo.OSInfo{ID: "debian", VersionID: "12"}, want: "debian"},
		{name: "debian 13", info: &osinfo.OSInfo{ID: "debian", VersionID: "13"}, want: "debian"},
		{name: "ubuntu 22.04", info: &osinfo.OSInfo{ID: "ubuntu", VersionID: "22.04"}, want: "ubuntu"},
		{name: "ubuntu 24.04", info: &osinfo.OSInfo{ID: "ubuntu", VersionID: "24.04"}, want: "ubuntu"},
		{name: "centos rejected", info: &osinfo.OSInfo{ID: "centos", VersionID: "8"}, wantErr: true},
		{name: "nil rejected", info: nil, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dockerURLPrefix(tc.info)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("dockerURLPrefix() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("dockerURLPrefix() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("dockerURLPrefix() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDockerSourceLine(t *testing.T) {
	tests := []struct {
		name       string
		distroSlug string
		codename   string
		arch       string
		keyring    string
		want       string
	}{
		{
			name:       "debian 12 amd64",
			distroSlug: "debian",
			codename:   "bookworm",
			arch:       "amd64",
			keyring:    "/etc/apt/keyrings/docker.gpg",
			want:       "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable\n",
		},
		{
			name:       "ubuntu 22.04 amd64",
			distroSlug: "ubuntu",
			codename:   "jammy",
			arch:       "amd64",
			keyring:    "/etc/apt/keyrings/docker.gpg",
			want:       "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu jammy stable\n",
		},
		{
			name:       "ubuntu 24.04 arm64",
			distroSlug: "ubuntu",
			codename:   "noble",
			arch:       "arm64",
			keyring:    "/etc/apt/keyrings/docker.gpg",
			want:       "deb [arch=arm64 signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu noble stable\n",
		},
		{
			name:       "debian 13 amd64",
			distroSlug: "debian",
			codename:   "trixie",
			arch:       "amd64",
			keyring:    "/etc/apt/keyrings/docker.gpg",
			want:       "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian trixie stable\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DockerSourceLine(tc.distroSlug, tc.codename, tc.arch, tc.keyring)
			if got != tc.want {
				t.Errorf("DockerSourceLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestK8sRepoBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		mirror string
		minor  string
		want   string
	}{
		{
			name:  "official v1.35",
			minor: "v1.35",
			want:  "https://pkgs.k8s.io/core:/stable:/v1.35/deb/",
		},
		{
			name:   "cn mirror v1.35",
			mirror: "cn",
			minor:  "v1.35",
			want:   "https://mirrors.aliyun.com/kubernetes-new/core:/stable:/v1.35/deb/",
		},
		{
			name:  "empty mirror = official",
			minor: "v1.34",
			want:  "https://pkgs.k8s.io/core:/stable:/v1.34/deb/",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := K8sRepoBaseURL(tc.mirror, tc.minor)
			if got != tc.want {
				t.Errorf("K8sRepoBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestK8sSourceLine(t *testing.T) {
	got := K8sSourceLine("", "v1.35", "/etc/apt/keyrings/kubernetes.gpg")
	want := "deb [signed-by=/etc/apt/keyrings/kubernetes.gpg] https://pkgs.k8s.io/core:/stable:/v1.35/deb/ /\n"
	if got != want {
		t.Errorf("K8sSourceLine(official) = %q, want %q", got, want)
	}

	gotCN := K8sSourceLine("cn", "v1.35", "/etc/apt/keyrings/kubernetes.gpg")
	wantCN := "deb [signed-by=/etc/apt/keyrings/kubernetes.gpg] https://mirrors.aliyun.com/kubernetes-new/core:/stable:/v1.35/deb/ /\n"
	if gotCN != wantCN {
		t.Errorf("K8sSourceLine(cn) = %q, want %q", gotCN, wantCN)
	}
}
