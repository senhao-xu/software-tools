package kube

import "testing"

func TestMinorVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"v-prefix patch", "v1.35.0", "v1.35"},
		{"v-prefix no patch", "v1.35", "v1.35"},
		{"no v-prefix patch", "1.35.0", "1.35"},
		{"no v-prefix no patch", "1.35", "1.35"},
		{"single component v1", "v1", "v1"},
		{"empty string", "", ""},
		{"v-prefix patch extra", "v1.35.10", "v1.35"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := minorVersion(tc.in); got != tc.want {
				t.Errorf("minorVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRepoBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		mirror string
		minor  string
		want   string
	}{
		{
			name:   "official",
			mirror: "",
			minor:  "v1.35",
			want:   "https://pkgs.k8s.io/core:/stable:/v1.35/deb/",
		},
		{
			name:   "cn mirror",
			mirror: "cn",
			minor:  "v1.35",
			want:   "https://mirrors.aliyun.com/kubernetes-new/core:/stable:/v1.35/deb/",
		},
		{
			name:   "unknown mirror falls back to official",
			mirror: "xx",
			minor:  "v1.35",
			want:   "https://pkgs.k8s.io/core:/stable:/v1.35/deb/",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoBaseURL(tc.mirror, tc.minor); got != tc.want {
				t.Errorf("repoBaseURL(%q, %q) = %q, want %q",
					tc.mirror, tc.minor, got, tc.want)
			}
		})
	}
}
