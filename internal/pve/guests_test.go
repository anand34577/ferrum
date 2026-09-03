package pve

import "testing"

func TestStripCIDR(t *testing.T) {
	cases := map[string]string{
		"192.168.1.50/24":              "192.168.1.50",
		"fe80::1234:5678:9abc:def0/64": "fe80::1234:5678:9abc:def0",
		"":                             "",
		"no-slash-here":                "no-slash-here",
	}
	for in, want := range cases {
		if got := stripCIDR(in); got != want {
			t.Errorf("stripCIDR(%q) = %q, want %q", in, got, want)
		}
	}
}
