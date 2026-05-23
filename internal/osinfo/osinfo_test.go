package osinfo

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *OSInfo
	}{
		{
			name: "debian 12 bookworm",
			content: `PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
NAME="Debian GNU/Linux"
VERSION_ID="12"
VERSION="12 (bookworm)"
VERSION_CODENAME=bookworm
ID=debian
`,
			want: &OSInfo{ID: "debian", VersionID: "12", Codename: "bookworm"},
		},
		{
			name: "debian 13 trixie",
			content: `PRETTY_NAME="Debian GNU/Linux 13 (trixie)"
NAME="Debian GNU/Linux"
VERSION_ID="13"
VERSION_CODENAME="trixie"
ID="debian"
`,
			want: &OSInfo{ID: "debian", VersionID: "13", Codename: "trixie"},
		},
		{
			name: "ubuntu",
			content: `NAME="Ubuntu"
VERSION="22.04.4 LTS (Jammy Jellyfish)"
ID=ubuntu
VERSION_ID="22.04"
VERSION_CODENAME=jammy
`,
			want: &OSInfo{ID: "ubuntu", VersionID: "22.04", Codename: "jammy"},
		},
		{
			name: "missing fields",
			content: `NAME="Whatever"
ID=mystery
`,
			want: &OSInfo{ID: "mystery"},
		},
		{
			name:    "empty file",
			content: "",
			want:    &OSInfo{},
		},
		{
			name: "comments and blanks ignored",
			content: `# a comment
ID=debian

VERSION_ID=12
# trailing comment
VERSION_CODENAME=bookworm
`,
			want: &OSInfo{ID: "debian", VersionID: "12", Codename: "bookworm"},
		},
		{
			name: "single quotes stripped",
			content: `ID='debian'
VERSION_ID='13'
VERSION_CODENAME='trixie'
`,
			want: &OSInfo{ID: "debian", VersionID: "13", Codename: "trixie"},
		},
		{
			name: "unknown keys ignored",
			content: `ID=debian
HOME_URL="https://www.debian.org/"
BUG_REPORT_URL="https://bugs.debian.org/"
VERSION_ID=12
`,
			want: &OSInfo{ID: "debian", VersionID: "12"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOSRelease(tc.content)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseOSRelease() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRequireSupported(t *testing.T) {
	tests := []struct {
		name      string
		info      *OSInfo
		wantErr   bool
		errSubstr string
	}{
		{
			name: "debian 12 ok",
			info: &OSInfo{ID: "debian", VersionID: "12"},
		},
		{
			name: "debian 13 ok",
			info: &OSInfo{ID: "debian", VersionID: "13"},
		},
		{
			name: "ubuntu 22.04 ok",
			info: &OSInfo{ID: "ubuntu", VersionID: "22.04"},
		},
		{
			name: "ubuntu 24.04 ok",
			info: &OSInfo{ID: "ubuntu", VersionID: "24.04"},
		},
		{
			name:      "nil info",
			info:      nil,
			wantErr:   true,
			errSubstr: "nil",
		},
		{
			name:      "centos rejected",
			info:      &OSInfo{ID: "centos", VersionID: "8"},
			wantErr:   true,
			errSubstr: "unsupported distro",
		},
		{
			name:      "rhel rejected",
			info:      &OSInfo{ID: "rhel", VersionID: "9"},
			wantErr:   true,
			errSubstr: "unsupported distro",
		},
		{
			name:      "linuxmint rejected (no ID_LIKE fallback per PRD)",
			info:      &OSInfo{ID: "linuxmint", VersionID: "21"},
			wantErr:   true,
			errSubstr: "unsupported distro",
		},
		{
			name:      "raspbian rejected (no ID_LIKE fallback per PRD)",
			info:      &OSInfo{ID: "raspbian", VersionID: "12"},
			wantErr:   true,
			errSubstr: "unsupported distro",
		},
		{
			name:      "debian 11 rejected",
			info:      &OSInfo{ID: "debian", VersionID: "11"},
			wantErr:   true,
			errSubstr: "unsupported debian VERSION_ID",
		},
		{
			name:      "debian empty version rejected",
			info:      &OSInfo{ID: "debian", VersionID: ""},
			wantErr:   true,
			errSubstr: "unsupported debian VERSION_ID",
		},
		{
			name:      "ubuntu 20.04 rejected (EOL per PRD)",
			info:      &OSInfo{ID: "ubuntu", VersionID: "20.04"},
			wantErr:   true,
			errSubstr: "unsupported ubuntu VERSION_ID",
		},
		{
			name:      "ubuntu 25.04 rejected (non-LTS, out of matrix)",
			info:      &OSInfo{ID: "ubuntu", VersionID: "25.04"},
			wantErr:   true,
			errSubstr: "unsupported ubuntu VERSION_ID",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := RequireSupported(tc.info)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("RequireSupported() = nil, want error containing %q", tc.errSubstr)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("RequireSupported() error = %q, want substring %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("RequireSupported() = %v, want nil", err)
			}
		})
	}
}
