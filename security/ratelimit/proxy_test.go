package ratelimit_test

import (
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/security/ratelimit"
)

func TestTrustedProxyParsingIsExplicitAndDefensive(t *testing.T) {
	empty, err := ratelimit.ParseTrustedProxies()
	if err != nil {
		t.Fatal(err)
	}
	if empty.Contains(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("zero configuration trusted a peer")
	}

	trusted, err := ratelimit.ParseTrustedProxies("10.0.0.0/8, 2001:db8::/32", "::ffff:192.0.2.0/120")
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"10.2.3.4", "2001:db8::1", "192.0.2.42"} {
		if !trusted.Contains(netip.MustParseAddr(address)) {
			t.Fatalf("trusted proxies do not contain %s", address)
		}
	}
	if trusted.Contains(netip.MustParseAddr("198.51.100.1")) {
		t.Fatal("trusted proxies accepted an address outside configured CIDRs")
	}

	for _, encoded := range []string{"10.0.0.1", "not-a-cidr", "10.0.0.0/8,,192.0.2.0/24"} {
		if _, err := ratelimit.ParseTrustedProxies(encoded); err == nil {
			t.Fatalf("ParseTrustedProxies(%q) succeeded", encoded)
		}
	}
	if _, err := ratelimit.ParseTrustedProxies(strings.Repeat("10.0.0.0/8,", 256) + "10.0.0.0/8"); err == nil {
		t.Fatal("trusted proxy parser accepted an unbounded prefix list")
	}
}

func TestUntrustedPeerCannotSupplyClientAddress(t *testing.T) {
	trusted, err := ratelimit.ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "198.51.100.20:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.Header.Set("Forwarded", "for=malformed")

	address, err := trusted.ResolveClientAddress(request)
	if err != nil || address != "198.51.100.20" {
		t.Fatalf("untrusted peer resolved to %q, %v", address, err)
	}
}

func TestTrustedProxyWalksChainFromTheSocketInward(t *testing.T) {
	trusted, err := ratelimit.ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "one proxy", header: "198.51.100.7", want: "198.51.100.7"},
		{name: "proxy chain", header: "198.51.100.7, 10.1.2.3", want: "198.51.100.7"},
		{name: "spoofed left entry", header: "203.0.113.99, 198.51.100.7", want: "198.51.100.7"},
		{name: "IPv6 client", header: "2001:db8:1::7, 10.1.2.3", want: "2001:db8:1::7"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = "10.9.8.7:443"
			request.Header.Set("X-Forwarded-For", test.header)
			address, err := trusted.ResolveClientAddress(request)
			if err != nil || address != test.want {
				t.Fatalf("resolved address = %q, %v; want %q", address, err, test.want)
			}
		})
	}
}

func TestTrustedProxyParsesStandardForwardedHeader(t *testing.T) {
	trusted, err := ratelimit.ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.9:443"
	request.Header.Set("Forwarded", `for="[2001:db8:2::4]:1234";proto=https, for=10.0.0.8;by=10.0.0.9`)
	address, err := trusted.ResolveClientAddress(request)
	if err != nil || address != "2001:db8:2::4" {
		t.Fatalf("Forwarded address = %q, %v", address, err)
	}
}

func TestMalformedOrAmbiguousForwardingFallsBackToDirectPeer(t *testing.T) {
	trusted, err := ratelimit.ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		forwarded []string
		legacy    []string
	}{
		{name: "both conventions", forwarded: []string{"for=198.51.100.1"}, legacy: []string{"198.51.100.1"}},
		{name: "repeated field", legacy: []string{"198.51.100.1", "198.51.100.2"}},
		{name: "empty then valid legacy field", legacy: []string{"", "198.51.100.1"}},
		{name: "empty then valid Forwarded field", forwarded: []string{"", "for=198.51.100.1"}},
		{name: "single empty field", legacy: []string{""}},
		{name: "empty chain member", legacy: []string{"198.51.100.1,,10.0.0.2"}},
		{name: "unknown node", forwarded: []string{"for=unknown"}},
		{name: "duplicate for", forwarded: []string{"for=198.51.100.1;for=198.51.100.2"}},
		{name: "invalid parameter name", forwarded: []string{"for=198.51.100.1;bad name=value"}},
		{name: "control character", forwarded: []string{"for=198.51.100.1;proto=ba\td"}},
		{name: "unterminated quote", forwarded: []string{`for="198.51.100.1`}},
		{name: "oversized", legacy: []string{strings.Repeat("1", 9000)}},
		{name: "too many hops", legacy: []string{strings.Repeat("198.51.100.1,", 64) + "198.51.100.1"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = "10.0.0.9:443"
			for _, value := range test.forwarded {
				request.Header.Add("Forwarded", value)
			}
			for _, value := range test.legacy {
				request.Header.Add("X-Forwarded-For", value)
			}
			address, err := trusted.ResolveClientAddress(request)
			if address != "10.0.0.9" || !errors.Is(err, ratelimit.ErrInvalidForwardedChain) {
				t.Fatalf("diagnostic resolution = %q, %v", address, err)
			}
			if fallback := trusted.ClientAddress(request); fallback != "10.0.0.9" {
				t.Fatalf("safe fallback = %q", fallback)
			}
		})
	}
}
