package httpguard

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestParseProxies(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantErr string
	}{
		{name: "empty trusts nobody", in: "", want: nil},
		{name: "only spaces and commas", in: " , ,", want: nil},
		{name: "single address", in: "127.0.0.1", want: []string{"127.0.0.1/32"}},
		{name: "ipv6 address", in: "::1", want: []string{"::1/128"}},
		{name: "mapped ipv4 becomes ipv4", in: "::ffff:10.1.2.3", want: []string{"10.1.2.3/32"}},
		{name: "address with zone", in: "fe80::1%en0", want: []string{"fe80::1/128"}},
		{name: "networks are masked", in: "10.1.2.3/8, fd00::1/8", want: []string{"10.0.0.0/8", "fd00::/8"}},
		{name: "spaces around items", in: "  192.168.0.0/16 ,127.0.0.1 ", want: []string{"192.168.0.0/16", "127.0.0.1/32"}},
		{name: "bad address", in: "10.0.0.300", wantErr: `trusted proxy "10.0.0.300"`},
		{name: "host name", in: "proxy.local", wantErr: `trusted proxy "proxy.local"`},
		{name: "bad network", in: "10.0.0.0/33", wantErr: `trusted proxy "10.0.0.0/33"`},
		{name: "every ipv4 address", in: "0.0.0.0/0", wantErr: "trusts every address"},
		{name: "every ipv6 address", in: "127.0.0.1, ::/0", wantErr: "trusts every address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseProxies(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, n := range p.nets {
				got = append(got, n.String())
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("nets = %v, want %v", got, tc.want)
			}
			if p.Empty() != (len(tc.want) == 0) {
				t.Fatalf("Empty() = %v", p.Empty())
			}
		})
	}
}

func mustProxies(t *testing.T, s string) Proxies {
	t.Helper()
	p, err := ParseProxies(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Настоящий адрес (T-06): заголовку X-Forwarded-For верим, только если соединение пришло от доверенного прокси,
// и читаем его справа налево до первого недоверенного адреса.
func TestClientAddr(t *testing.T) {
	proxies := mustProxies(t, "127.0.0.1, 10.0.0.0/8, fd00::/8")
	cases := []struct {
		name    string
		proxies Proxies
		remote  string
		xff     []string
		want    string
	}{
		{name: "no proxies: connection address", remote: "203.0.113.7:4711", want: "203.0.113.7"},
		{name: "no proxies: header ignored", remote: "203.0.113.7:4711", xff: []string{"198.51.100.1"}, want: "203.0.113.7"},
		{name: "untrusted connection: header ignored", proxies: proxies, remote: "203.0.113.7:4711", xff: []string{"198.51.100.1"}, want: "203.0.113.7"},
		{name: "trusted proxy without header", proxies: proxies, remote: "127.0.0.1:5000", want: "127.0.0.1"},
		{name: "trusted proxy with client", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"198.51.100.1"}, want: "198.51.100.1"},
		{name: "forged left part is ignored", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"1.1.1.1, 198.51.100.1"}, want: "198.51.100.1"},
		{name: "chain of trusted proxies", proxies: proxies, remote: "10.0.0.2:5000", xff: []string{"198.51.100.1, 10.0.0.9, 10.0.0.5"}, want: "198.51.100.1"},
		{name: "several header lines in order", proxies: proxies, remote: "10.0.0.2:5000", xff: []string{"1.1.1.1", "198.51.100.1, 10.0.0.9"}, want: "198.51.100.1"},
		{name: "everything trusted: leftmost", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"10.0.0.3, 10.0.0.4"}, want: "10.0.0.3"},
		{name: "hop with port", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"198.51.100.1:6000"}, want: "198.51.100.1"},
		{name: "ipv6 hop with port", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"[2001:db8::5]:6000"}, want: "2001:db8::5"},
		{name: "ipv6 proxy and client", proxies: proxies, remote: "[fd00::1]:5000", xff: []string{"2001:db8::5"}, want: "2001:db8::5"},
		{name: "mapped ipv4 client", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"::ffff:198.51.100.1"}, want: "198.51.100.1"},
		{name: "garbage right after proxy", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"198.51.100.1, unknown"}, want: "127.0.0.1"},
		{name: "garbage behind a trusted hop", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{"evil, 10.0.0.3"}, want: "10.0.0.3"},
		{name: "empty entries", proxies: proxies, remote: "127.0.0.1:5000", xff: []string{" , "}, want: "127.0.0.1"},
		{name: "connection without port", remote: "203.0.113.7", want: "203.0.113.7"},
		{name: "unparsable connection", proxies: proxies, remote: "pipe", xff: []string{"198.51.100.1"}, want: "invalid IP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := tc.proxies.clientAddr(r).String(); got != tc.want {
				t.Fatalf("client = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:4711"
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Fatalf("without perimeter = %q", got)
	}
	if got := ClientIP(withClient(r, netip.MustParseAddr("198.51.100.1"))); got != "198.51.100.1" {
		t.Fatalf("after perimeter = %q", got)
	}
	r.RemoteAddr = "@"
	if got := ClientIP(r); got != "@" {
		t.Fatalf("unparsable = %q, want the raw address", got)
	}
}
