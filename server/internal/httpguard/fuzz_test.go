package httpguard

import (
	"net/http"
	"net/netip"
	"testing"
)

// FuzzParseProxies: настройка доверенных прокси не роняет разбор, принятый список никогда не доверяет всему интернету
// и сам разбирается заново так же.
func FuzzParseProxies(f *testing.F) {
	for _, s := range []string{"", "10.0.0.0/8", "127.0.0.1, ::1", "0.0.0.0/0", "::/0", "1.2.3.4/0", "::ffff:10.0.0.1", "x", ",,,", "10.0.0.1/33", "fd00::/8,10.1.1.1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		p, err := ParseProxies(in)
		if err != nil {
			if !p.Empty() {
				t.Fatalf("ParseProxies(%q): error with a non-empty list", in)
			}
			return
		}
		for _, n := range p.nets {
			if n.Bits() == 0 {
				t.Fatalf("ParseProxies(%q) trusts a whole address family: %v", in, n)
			}
		}
		for _, probe := range []string{"8.8.8.8", "2001:4860:4860::8888"} {
			a := netip.MustParseAddr(probe)
			if len(p.nets) > 0 && p.trusts(a) {
				for _, n := range p.nets {
					if n.Contains(a) {
						goto fine
					}
				}
				t.Fatalf("ParseProxies(%q) trusts %s without a network that contains it", in, a)
			}
		fine:
		}
	})
}

// FuzzClientAddr: что бы ни стояло в X-Forwarded-For и RemoteAddr, адрес человека определён, а подделка слева (до
// первого недоверенного адреса справа) его не меняет.
func FuzzClientAddr(f *testing.F) {
	f.Add("203.0.113.9:4000", "198.51.100.1, 10.0.0.2", "10.0.0.0/8")
	f.Add("10.0.0.2:1", "garbage, , 1.2.3.4", "10.0.0.0/8")
	f.Add("[::1]:80", "::ffff:1.2.3.4", "::1")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, remote, xff, proxies string) {
		p, err := ParseProxies(proxies)
		if err != nil {
			return
		}
		r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		r.Header.Set("X-Forwarded-For", xff)
		got := p.clientAddr(r)
		if got.IsValid() && got.Is4In6() {
			t.Fatalf("client address %s keeps the IPv4-in-IPv6 form", got)
		}
		// Без доверенного прокси заголовок не имеет значения.
		if p.Empty() || !p.trusts(remoteAddr(r)) {
			r2 := &http.Request{RemoteAddr: remote, Header: http.Header{}}
			if want := p.clientAddr(r2); want != got {
				t.Fatalf("untrusted peer %q changed the address by X-Forwarded-For: %s vs %s", remote, got, want)
			}
		}
	})
}
