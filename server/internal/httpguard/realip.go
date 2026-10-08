package httpguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Proxies — сети, которым сервер верит заголовок X-Forwarded-For (настройка SCIBOX_TRUSTED_PROXIES, T-06).
// Нулевое значение не доверяет никому: адрес человека берётся из соединения, заголовки прокси игнорируются.
type Proxies struct {
	nets []netip.Prefix
}

// ParseProxies разбирает список через запятую: сети (10.0.0.0/8, fd00::/8) и отдельные адреса (127.0.0.1, ::1).
// Пустая строка — никому не доверять. Сеть /0 (весь интернет) не принимается: тогда любой посетитель мог бы
// назваться чужим адресом и обойти ограничения частоты.
func ParseProxies(s string) (Proxies, error) {
	var p Proxies
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var prefix netip.Prefix
		if strings.Contains(part, "/") {
			parsed, err := netip.ParsePrefix(part)
			if err != nil {
				return Proxies{}, fmt.Errorf("trusted proxy %q: want an address or a network like 10.0.0.0/8: %w", part, err)
			}
			prefix = parsed.Masked()
		} else {
			addr, err := netip.ParseAddr(part)
			if err != nil {
				return Proxies{}, fmt.Errorf("trusted proxy %q: want an address or a network like 10.0.0.0/8: %w", part, err)
			}
			addr = plain(addr)
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		if prefix.Bits() == 0 {
			return Proxies{}, fmt.Errorf("trusted proxy %q trusts every address: anyone could forge X-Forwarded-For", part)
		}
		p.nets = append(p.nets, prefix)
	}
	return p, nil
}

// Empty сообщает, что доверенных прокси нет.
func (p Proxies) Empty() bool { return len(p.nets) == 0 }

func (p Proxies) trusts(a netip.Addr) bool {
	for _, n := range p.nets {
		if n.Contains(a) {
			return true
		}
	}
	return false
}

// clientAddr — адрес человека. Если соединение пришло от доверенного прокси, X-Forwarded-For читается
// справа налево: правые записи добавили наши прокси, первая недоверенная — тот, кто к ним пришёл.
// Всё, что левее, мог написать сам посетитель, поэтому туда не смотрим. Испорченная запись
// останавливает чтение: верим последнему адресу, который прочитали надёжно.
func (p Proxies) clientAddr(r *http.Request) netip.Addr {
	client := remoteAddr(r)
	if !client.IsValid() || !p.trusts(client) {
		return client
	}
	hops := forwardedFor(r.Header)
	for i := len(hops) - 1; i >= 0; i-- {
		a, ok := parseHop(hops[i])
		if !ok {
			return client
		}
		client = a
		if !p.trusts(a) {
			return a
		}
	}
	return client
}

// forwardedFor собирает записи X-Forwarded-For из всех строк заголовка по порядку.
func forwardedFor(h http.Header) []string {
	var hops []string
	for _, line := range h.Values("X-Forwarded-For") {
		for hop := range strings.SplitSeq(line, ",") {
			hops = append(hops, strings.TrimSpace(hop))
		}
	}
	return hops
}

// parseHop понимает адрес с портом и без: 203.0.113.7, 203.0.113.7:4711, 2001:db8::1, [2001:db8::1]:4711.
func parseHop(s string) (netip.Addr, bool) {
	if a, err := netip.ParseAddr(s); err == nil {
		return plain(a), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return plain(ap.Addr()), true
	}
	return netip.Addr{}, false
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return plain(a)
}

// plain убирает зону (fe80::1%en0) и превращает IPv4 в записи IPv6 (::ffff:1.2.3.4) в обычный IPv4,
// чтобы один человек не получал два разных ключа.
func plain(a netip.Addr) netip.Addr {
	return a.WithZone("").Unmap()
}

type clientKey struct{}

// withClient кладёт адрес человека в контекст запроса.
func withClient(r *http.Request, a netip.Addr) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), clientKey{}, a))
}

// ClientIP — адрес человека для лимитов, сессий и журналов. После Perimeter это адрес с учётом доверенных прокси;
// без него — адрес соединения. Если адрес не разобрать, возвращается RemoteAddr как есть.
func ClientIP(r *http.Request) string {
	if a := clientIP(r); a.IsValid() {
		return a.String()
	}
	return r.RemoteAddr
}

func clientIP(r *http.Request) netip.Addr {
	if a, ok := r.Context().Value(clientKey{}).(netip.Addr); ok {
		return a
	}
	return remoteAddr(r)
}
