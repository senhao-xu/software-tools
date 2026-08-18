package cli

import (
	"strings"
	"testing"
)

func TestResolveInstallPackages(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr bool
	}{
		{
			name: "alias expansion",
			args: []string{"python"},
			want: []string{"python3", "python3-pip", "python-is-python3"},
		},
		{
			name: "nodejs alias maps to nodejs package",
			args: []string{"nodejs"},
			want: []string{"nodejs"},
		},
		{
			name: "java alias maps to temurin-21-jdk",
			args: []string{"java"},
			want: []string{"temurin-21-jdk"},
		},
		{
			name: "maven alias maps to maven package",
			args: []string{"maven"},
			want: []string{"maven"},
		},
		{
			name: "java and maven merge into one list",
			args: []string{"java", "maven"},
			want: []string{"temurin-21-jdk", "maven"},
		},
		{
			name: "nodejs with passthrough merges into one list",
			args: []string{"nodejs", "htop"},
			want: []string{"nodejs", "htop"},
		},
		{
			name: "unknown name passes through verbatim",
			args: []string{"htop"},
			want: []string{"htop"},
		},
		{
			name: "alias and passthrough merge into one list",
			args: []string{"python", "htop"},
			want: []string{"python3", "python3-pip", "python-is-python3", "htop"},
		},
		{
			name: "duplicate args deduplicated preserving order",
			args: []string{"htop", "python", "htop"},
			want: []string{"htop", "python3", "python3-pip", "python-is-python3"},
		},
		{
			name:    "docker is reserved",
			args:    []string{"docker"},
			wantErr: true,
		},
		{
			name:    "k8s is reserved",
			args:    []string{"k8s"},
			wantErr: true,
		},
		{
			name:    "reserved name rejected even alongside other packages",
			args:    []string{"htop", "docker"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveInstallPackages(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveInstallPackages(%v) = %v, want error", tt.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveInstallPackages(%v) error: %v", tt.args, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("resolveInstallPackages(%v) = %v, want %v", tt.args, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("resolveInstallPackages(%v) = %v, want %v", tt.args, got, tt.want)
				}
			}
		})
	}
}

func TestCollectInstallPreHooks(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "nodejs has the NodeSource setup hook",
			args: []string{"nodejs"},
			want: []string{"curl -fsSL https://deb.nodesource.com/setup_22.x | bash -"},
		},
		{
			name: "hook runs once even with duplicate args",
			args: []string{"nodejs", "nodejs"},
			want: []string{"curl -fsSL https://deb.nodesource.com/setup_22.x | bash -"},
		},
		{
			name: "hookless names return no hooks",
			args: []string{"python", "htop"},
			want: nil,
		},
		{
			name: "mixed args keep only names with hooks",
			args: []string{"htop", "nodejs"},
			want: []string{"curl -fsSL https://deb.nodesource.com/setup_22.x | bash -"},
		},
		{
			name: "java has the Adoptium repo setup hook",
			args: []string{"java"},
			want: []string{installPreHooks["java"]},
		},
		{
			name: "java and nodejs hooks both collected in order",
			args: []string{"java", "nodejs"},
			want: []string{installPreHooks["java"], installPreHooks["nodejs"]},
		},
		{
			name: "java hook runs once even with duplicate args",
			args: []string{"java", "maven", "java"},
			want: []string{installPreHooks["java"]},
		},
		{
			name: "hookless names including maven return no hooks",
			args: []string{"python", "maven", "htop"},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectInstallPreHooks(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("collectInstallPreHooks(%v) = %v, want %v", tt.args, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("collectInstallPreHooks(%v) = %v, want %v", tt.args, got, tt.want)
				}
			}
		})
	}
}

func TestNeedsPostHookUpdate(t *testing.T) {
	if !needsPostHookUpdate([]string{"some hook"}) {
		t.Fatal("needsPostHookUpdate with one hook = false, want true")
	}
	if !needsPostHookUpdate(collectInstallPreHooks([]string{"java"})) {
		t.Fatal("needsPostHookUpdate for java = false, want true (Adoptium sources must be indexed)")
	}
	if needsPostHookUpdate(collectInstallPreHooks([]string{"python", "maven", "htop"})) {
		t.Fatal("needsPostHookUpdate with no hooks = true, want false")
	}
	if needsPostHookUpdate(nil) {
		t.Fatal("needsPostHookUpdate(nil) = true, want false")
	}
}

func TestInstallReservedNamesIncludeHint(t *testing.T) {
	for _, name := range []string{"docker", "k8s"} {
		_, err := resolveInstallPackages([]string{name})
		if err == nil {
			t.Fatalf("resolveInstallPackages(%q) should error", name)
		}
		want := "xsh " + name
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("resolveInstallPackages(%q) error %q should hint at %q", name, err.Error(), want)
		}
	}
}

func TestInstallCmdRequiresArgs(t *testing.T) {
	cmd := NewInstallCmd()
	cmd.SetArgs([]string{})
	// Silence output; we only care about the usage error.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("install with no args should fail with a usage error")
	}
}

func TestInstallCmdRegisteredShape(t *testing.T) {
	cmd := NewInstallCmd()
	if cmd.Name() != "install" {
		t.Fatalf("command name = %q, want %q", cmd.Name(), "install")
	}
	if cmd.Args == nil {
		t.Fatal("install command must declare an Args validator")
	}
	if err := cmd.Args(cmd, []string{"htop"}); err != nil {
		t.Fatalf("Args should accept one package: %v", err)
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Fatal("Args should reject empty args")
	}
}
