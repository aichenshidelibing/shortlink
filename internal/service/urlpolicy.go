package service

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

type NormalizedURL struct {
	URL  string
	Host string
}

func NormalizeDestinationURL(raw string, allowPrivate bool) (*NormalizedURL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("url is required")
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return nil, fmt.Errorf("url contains invalid whitespace or control characters")
		}
	}

	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "//") {
		s = "https:" + s
	} else if !strings.Contains(s, "://") {
		if i := strings.Index(s, ":"); i > 0 {
			prefix := strings.ToLower(s[:i])
			if prefix != "localhost" && net.ParseIP(prefix) == nil && !strings.Contains(prefix, ".") {
				return nil, fmt.Errorf("unsupported url scheme")
			}
		}
		s = "https://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("url host is required")
	}
	if u.User != nil {
		return nil, fmt.Errorf("url userinfo is not allowed")
	}

	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return nil, fmt.Errorf("url host is required")
	}
	asciiHost, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return nil, fmt.Errorf("invalid domain name")
	}
	asciiHost = strings.ToLower(strings.TrimSuffix(asciiHost, "."))
	if !allowPrivate && isLocalHostname(asciiHost) {
		return nil, fmt.Errorf("private or local host destinations are not allowed")
	}

	if !allowPrivate {
		ip := net.ParseIP(asciiHost)
		if ip == nil && looksLikeIPv4Literal(asciiHost) {
			return nil, fmt.Errorf("non-standard IP literal destinations are not allowed")
		}
		if ip != nil && isBlockedDestinationIP(ip) {
			return nil, fmt.Errorf("private or local IP destinations are not allowed")
		}
	}

	port := u.Port()
	hostport := asciiHost
	if port != "" && !((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
		hostport = net.JoinHostPort(asciiHost, port)
	}
	u.Scheme = scheme
	u.Host = hostport
	return &NormalizedURL{URL: u.String(), Host: asciiHost}, nil
}

func isBlockedDestinationIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func looksLikeIPv4Literal(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if !isIPv4Number(part) {
			return false
		}
	}
	return true
}

func isIPv4Number(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "0x") {
		if len(s) == 2 {
			return false
		}
		_, err := strconv.ParseUint(s[2:], 16, 32)
		return err == nil
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(s, 10, 32)
	return err == nil
}

func isLocalHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home")
}
