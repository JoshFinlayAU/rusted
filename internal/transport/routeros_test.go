package transport

import (
	"reflect"
	"testing"
)

func TestAPIWords(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"/export", []string{"/export"}},
		{"/export terse", []string{"/export", "=terse="}},                                  // bare flag -> =flag=
		{"/ip/address/print ?disabled=no", []string{"/ip/address/print", "?disabled=no"}},  // query passed through
		{"/system/resource/print =detail=", []string{"/system/resource/print", "=detail="}}, // already API-shaped
		{"", nil},
	}
	for _, c := range cases {
		if got := apiWords(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("apiWords(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRouterOSRegistered(t *testing.T) {
	tr, err := Get("routeros-api")
	if err != nil {
		t.Fatalf("routeros-api transport not registered: %v", err)
	}
	if tr.Name() != "routeros-api" {
		t.Errorf("Name() = %q, want routeros-api", tr.Name())
	}
}
