package sanitize

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"
)

func validPort(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(s, 10, 16)
	return err == nil && n <= 65535
}
func hexDigit(b byte) int {
	if b >= '0' && b <= '9' {
		return int(b - '0')
	}
	if b >= 'a' && b <= 'f' {
		return int(b-'a') + 10
	}
	if b >= 'A' && b <= 'F' {
		return int(b-'A') + 10
	}
	return -1
}
func decodedHost(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) {
				return "", false
			}
			a, c := hexDigit(s[i+1]), hexDigit(s[i+2])
			if a < 0 || c < 0 {
				return "", false
			}
			b.WriteByte(byte(a*16 + c))
			i += 2
		} else {
			b.WriteByte(s[i])
		}
	}
	value := b.String()
	if !utf8.ValidString(value) {
		return "", false
	}
	for _, r := range value {
		if r <= 32 || r == 127 || strings.ContainsRune("#%/:<>?@[\\]^|", r) {
			return "", false
		}
	}
	return value, true
}
func ipv4Number(s string) (uint64, bool) {
	base := 10
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base = 16
		s = s[2:]
	} else if len(s) > 1 && s[0] == '0' {
		base = 8
		s = s[1:]
	}
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(s, base, 64)
	return n, err == nil || errors.Is(err, strconv.ErrRange)
}

// WHATWG の IPv4 の数表現（省略形・16進・8進）を、数値の上限も含めて確認する。
func validIPv4Domain(host string) bool {
	parts := strings.Split(strings.TrimSuffix(host, "."), ".")
	last := parts[len(parts)-1]
	_, number := ipv4Number(last)
	digits := last != ""
	for _, r := range last {
		if r < '0' || r > '9' {
			digits = false
		}
	}
	if !number && !digits {
		return true
	}
	if len(parts) > 4 {
		return false
	}
	for i, p := range parts {
		n, ok := ipv4Number(p)
		if !ok {
			return false
		}
		if i < len(parts)-1 && n > 255 {
			return false
		}
		if i == len(parts)-1 && n >= uint64(1)<<uint(8*(5-len(parts))) {
			return false
		}
	}
	return true
}
func validHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.HasPrefix(host, "[") {
		close := strings.IndexByte(host, ']')
		if close < 0 {
			return false
		}
		addr, err := netip.ParseAddr(host[1:close])
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return false
		}
		tail := host[close+1:]
		return tail == "" || strings.HasPrefix(tail, ":") && validPort(tail[1:])
	}
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
		if !validPort(host[colon+1:]) {
			return false
		}
		host = host[:colon]
	}
	domain, ok := decodedHost(host)
	return ok && domain != "" && validIPv4Domain(domain)
}
