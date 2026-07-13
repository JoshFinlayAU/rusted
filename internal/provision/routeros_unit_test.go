package provision

import "testing"

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"admin":   "admin",
		"nms":     "nms",
		"a b/c":   "a_b_c",
		"../evil": "___evil", // dots + slashes -> _, so no path traversal in the filename
		"":        "user",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
