package sysprep

import "testing"

func TestCommentSwapLines(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantOut     string
		wantChanged bool
	}{
		{
			name:        "empty content",
			in:          "",
			wantOut:     "",
			wantChanged: false,
		},
		{
			name:        "no swap entries",
			in:          "UUID=abc / ext4 defaults 0 1\n",
			wantOut:     "UUID=abc / ext4 defaults 0 1\n",
			wantChanged: false,
		},
		{
			name:        "already commented swap",
			in:          "UUID=abc / ext4 defaults 0 1\n#UUID=def none swap sw 0 0\n",
			wantOut:     "UUID=abc / ext4 defaults 0 1\n#UUID=def none swap sw 0 0\n",
			wantChanged: false,
		},
		{
			name:        "single uncommented swap",
			in:          "UUID=abc / ext4 defaults 0 1\nUUID=def none swap sw 0 0\n",
			wantOut:     "UUID=abc / ext4 defaults 0 1\n#UUID=def none swap sw 0 0\n",
			wantChanged: true,
		},
		{
			name: "mixed: one already commented, one uncommented",
			in: "UUID=abc / ext4 defaults 0 1\n" +
				"#UUID=old none swap sw 0 0\n" +
				"/swap.img none swap sw 0 0\n",
			wantOut: "UUID=abc / ext4 defaults 0 1\n" +
				"#UUID=old none swap sw 0 0\n" +
				"#/swap.img none swap sw 0 0\n",
			wantChanged: true,
		},
		{
			name: "uuid-style swap",
			in: "UUID=11111111-2222-3333-4444-555555555555 / ext4 errors=remount-ro 0 1\n" +
				"UUID=66666666-7777-8888-9999-aaaaaaaaaaaa none swap sw 0 0\n",
			wantOut: "UUID=11111111-2222-3333-4444-555555555555 / ext4 errors=remount-ro 0 1\n" +
				"#UUID=66666666-7777-8888-9999-aaaaaaaaaaaa none swap sw 0 0\n",
			wantChanged: true,
		},
		{
			name:        "no trailing newline preserved",
			in:          "UUID=def none swap sw 0 0",
			wantOut:     "#UUID=def none swap sw 0 0",
			wantChanged: true,
		},
		{
			name: "blank lines preserved",
			in: "UUID=abc / ext4 defaults 0 1\n\n" +
				"UUID=def none swap sw 0 0\n",
			wantOut: "UUID=abc / ext4 defaults 0 1\n\n" +
				"#UUID=def none swap sw 0 0\n",
			wantChanged: true,
		},
		{
			name: "non-swap third field untouched",
			in: "UUID=abc / ext4 defaults 0 1\n" +
				"tmpfs /tmp tmpfs defaults 0 0\n",
			wantOut: "UUID=abc / ext4 defaults 0 1\n" +
				"tmpfs /tmp tmpfs defaults 0 0\n",
			wantChanged: false,
		},
		{
			name:        "too few fields ignored",
			in:          "foo bar\n",
			wantOut:     "foo bar\n",
			wantChanged: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotOut, gotChanged := commentSwapLines(tc.in)
			if gotChanged != tc.wantChanged {
				t.Errorf("commentSwapLines() changed = %v, want %v", gotChanged, tc.wantChanged)
			}
			if gotOut != tc.wantOut {
				t.Errorf("commentSwapLines() out =\n%q\nwant\n%q", gotOut, tc.wantOut)
			}
		})
	}
}
