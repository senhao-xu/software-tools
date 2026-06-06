package dockerinstall

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseMadisonVersion(t *testing.T) {
	// A realistic block of `apt-cache madison docker-ce` output, with the
	// columns separated by " | " just as apt-cache emits them.
	multi := `docker-ce | 5:27.5.1-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
docker-ce | 5:27.5.0-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
docker-ce | 5:26.1.4-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
docker-ce | 5:25.0.5-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
`

	tests := []struct {
		name   string
		output string
		major  int
		want   string
	}{
		{
			name:   "single line major match",
			output: "docker-ce | 5:27.5.1-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages\n",
			major:  27,
			want:   "5:27.5.1-1~debian.12~bookworm",
		},
		{
			name:   "multi-line picks latest matching",
			output: multi,
			major:  27,
			want:   "5:27.5.1-1~debian.12~bookworm",
		},
		{
			name:   "multi-line picks older major",
			output: multi,
			major:  26,
			want:   "5:26.1.4-1~debian.12~bookworm",
		},
		{
			name:   "multi-line picks 25",
			output: multi,
			major:  25,
			want:   "5:25.0.5-1~debian.12~bookworm",
		},
		{
			name:   "no epoch present",
			output: "docker-ce | 24.0.7-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages\n",
			major:  24,
			want:   "24.0.7-1~debian.12~bookworm",
		},
		{
			name:   "no match returns empty",
			output: multi,
			major:  99,
			want:   "",
		},
		{
			name:   "empty output returns empty",
			output: "",
			major:  27,
			want:   "",
		},
		{
			name:   "junk lines ignored",
			output: "not a madison line\nanother bad\n",
			major:  27,
			want:   "",
		},
		{
			name: "epoch-stripped prefix does not bleed into version digits",
			// The epoch 5: must NOT confuse the major=5 search; major is
			// matched against the post-epoch version component, so
			// "27.5.1" only matches major=27.
			output: multi,
			major:  5,
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMadisonVersion(tc.output, tc.major)
			if got != tc.want {
				t.Errorf("parseMadisonVersion(major=%d) = %q, want %q", tc.major, got, tc.want)
			}
		})
	}
}

func TestParseMadisonVersions(t *testing.T) {
	output := `docker-ce | 5:27.5.1-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
docker-ce | 5:27.5.1-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
docker-ce | 5:26.1.4-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
docker-ce | 24.0.7-1~debian.12~bookworm | https://download.docker.com/linux/debian bookworm/stable amd64 Packages
junk
`

	versions := parseMadisonVersions(output, 0)
	if len(versions) != 3 {
		t.Fatalf("parseMadisonVersions() len = %d, want 3", len(versions))
	}
	if versions[0].Full != "5:27.5.1-1~debian.12~bookworm" || versions[0].Display != "27.5.1-1~debian.12~bookworm" {
		t.Fatalf("first version = %+v", versions[0])
	}
	if versions[2].Full != "24.0.7-1~debian.12~bookworm" || versions[2].Display != "24.0.7-1~debian.12~bookworm" {
		t.Fatalf("third version = %+v", versions[2])
	}

	filtered := parseMadisonVersions(output, 26)
	if len(filtered) != 1 {
		t.Fatalf("parseMadisonVersions(major=26) len = %d, want 1", len(filtered))
	}
	if filtered[0].Full != "5:26.1.4-1~debian.12~bookworm" {
		t.Fatalf("filtered[0] = %+v", filtered[0])
	}
}

func TestAskVersion(t *testing.T) {
	versions := []dockerVersion{
		{Full: "5:27.5.1-1~debian.12~bookworm", Display: "27.5.1-1~debian.12~bookworm"},
		{Full: "5:26.1.4-1~debian.12~bookworm", Display: "26.1.4-1~debian.12~bookworm"},
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "enter picks latest",
			input: "\n",
			want:  "5:27.5.1-1~debian.12~bookworm",
		},
		{
			name:  "number picks matching version",
			input: "2\n",
			want:  "5:26.1.4-1~debian.12~bookworm",
		},
		{
			name:  "invalid answers default latest after retries",
			input: "x\n0\n9\n",
			want:  "5:27.5.1-1~debian.12~bookworm",
		},
		{
			name:  "eof defaults latest",
			input: "",
			want:  "5:27.5.1-1~debian.12~bookworm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got := askVersion(strings.NewReader(tc.input), &out, versions)
			if got != tc.want {
				t.Fatalf("askVersion() = %q, want %q", got, tc.want)
			}
			if !strings.Contains(out.String(), "Available Docker CE versions:") {
				t.Fatalf("askVersion() output missing version list: %q", out.String())
			}
		})
	}
}
