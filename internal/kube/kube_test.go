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

// TestRepoBaseURL was removed in PR12: the URL-building logic moved to
// internal/aptrepo (covered by aptrepo.TestK8sRepoBaseURL with the same
// official/cn/unknown-mirror cases).
