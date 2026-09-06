package pve

import "testing"

func TestStatusErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "field errors map",
			body: `{"data":null,"errors":{"vmid":"value does not match the regex pattern"}}`,
			want: "vmid: value does not match the regex pattern",
		},
		{
			name: "multiple field errors sorted by key",
			body: `{"data":null,"errors":{"vmid":"bad vmid","node":"bad node"}}`,
			want: "node: bad node; vmid: bad vmid",
		},
		{
			name: "top-level message",
			body: `{"data":null,"message":"storage is not shared"}`,
			want: "storage is not shared",
		},
		{
			name: "not JSON — fall back to raw body",
			body: "502 Bad Gateway",
			want: "502 Bad Gateway",
		},
		{
			name: "JSON with neither field — fall back to raw body",
			body: `{"data":null}`,
			want: `{"data":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &StatusError{Method: "POST", Path: "/nodes/pve1/qemu", StatusCode: 400, Body: tc.body}
			if got := e.Message(); got != tc.want {
				t.Errorf("Message() = %q, want %q", got, tc.want)
			}
		})
	}
}
