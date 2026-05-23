package containerd

import (
	"strings"
	"testing"
)

func TestRenderConfigTOML(t *testing.T) {
	tests := []struct {
		name         string
		opts         Options
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "default mirror empty",
			opts: Options{Mirror: ""},
			wantContains: []string{
				`version = 2`,
				`sandbox_image = "registry.k8s.io/pause:3.10"`,
				`SystemdCgroup = true`,
				`runtime_type = "io.containerd.runc.v2"`,
			},
			wantAbsent: []string{
				`registry.aliyuncs.com`,
				`registry.mirrors`,
			},
		},
		{
			name: "mirror cn",
			opts: Options{Mirror: "cn"},
			wantContains: []string{
				`version = 2`,
				`sandbox_image = "registry.aliyuncs.com/google_containers/pause:3.10"`,
				`SystemdCgroup = true`,
				`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.k8s.io"]`,
				`endpoint = ["https://registry.aliyuncs.com/google_containers"]`,
			},
			wantAbsent: []string{
				`sandbox_image = "registry.k8s.io/pause:3.10"`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderConfigTOML(tc.opts)
			if err != nil {
				t.Fatalf("renderConfigTOML() error = %v", err)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("renderConfigTOML() missing expected fragment %q\nfull output:\n%s", want, got)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("renderConfigTOML() unexpectedly contained %q\nfull output:\n%s", absent, got)
				}
			}
		})
	}
}
