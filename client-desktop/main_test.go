package main

import "testing"

func TestParseSendFilter(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantText  bool
		wantImage bool
		wantFile  bool
		wantErr   bool
	}{
		{name: "default all", in: "all", wantText: true, wantImage: true, wantFile: true},
		{name: "none", in: "none", wantText: false, wantImage: false, wantFile: false},
		{name: "text only", in: "text", wantText: true, wantImage: false, wantFile: false},
		{name: "image only", in: "image", wantText: false, wantImage: true, wantFile: false},
		{name: "file only", in: "file", wantText: false, wantImage: false, wantFile: true},
		{name: "combo", in: "text,file", wantText: true, wantImage: false, wantFile: true},
		{name: "combo with spaces", in: " text , image ", wantText: true, wantImage: true, wantFile: false},
		{name: "invalid", in: "text,audio", wantErr: true},
		{name: "empty list", in: ",,", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotText, gotImage, gotFile, err := parseSendFilter(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if gotText != tc.wantText || gotImage != tc.wantImage || gotFile != tc.wantFile {
				t.Fatalf(
					"parseSendFilter(%q) = text=%v image=%v file=%v, want text=%v image=%v file=%v",
					tc.in, gotText, gotImage, gotFile, tc.wantText, tc.wantImage, tc.wantFile,
				)
			}
		})
	}
}
