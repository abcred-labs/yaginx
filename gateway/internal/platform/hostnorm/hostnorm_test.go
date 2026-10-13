package hostnorm

import "testing"

func TestNormalization(t *testing.T) {
	tests := []struct {
		name, input, want string
		ok                bool
		fn                func(string) (string, bool)
	}{
		{"domain", " BÜCHER.example. ", "xn--bcher-kva.example", true, Domain},
		{"wildcard rejected", "*.example.test", "", false, Domain},
		{"host port", "Example.TEST:443", "example.test", true, HostHeader},
		{"ipv6", "[2001:db8::1]", "2001:db8::1", true, HostHeader},
		{"authority", "HTTPS://Example.TEST:443", "example.test:443", true, Authority},
		{"unmatched", "__unmatched__", "", false, IPOrHost},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := test.fn(test.input)
			if got != test.want || ok != test.ok {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, test.want, test.ok)
			}
		})
	}
}
