package service

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

// SafeScanner checks URLs for malicious patterns WITHOUT making HTTP requests.
// Uses DNS-based checks, pattern heuristics, and offline keyword matching.
type SafeScanner struct {
	log *zap.Logger
}

func NewSafeScanner(log *zap.Logger) *SafeScanner {
	return &SafeScanner{log: log}
}

// ScanResult holds the outcome of a URL safety scan.
type ScanResult struct {
	Safe  bool     `json:"safe"`
	Risks []string `json:"risks,omitempty"`
	Score int      `json:"score"` // 0-100, higher = more risky
}

// ── Suspicious patterns (offline, no HTTP request) ──

var (
	// Phishing TLDs often abused
	suspiciousTLDs = map[string]bool{
		"tk": true, "ml": true, "ga": true, "cf": true, "gq": true, // free Freenom TLDs
		"xyz": true, "top": true, "work": true, "loan": true, "click": true,
		"country": true, "stream": true, "download": true, "review": true,
	}
	// Common phishing keywords in domain names
	phishingKeywords = []string{
		"login", "signin", "verify", "secure", "account", "update", "confirm",
		"banking", "password", "credential", "recovery", "unlock", "validate",
		"authenticate", "authorize", "wallet", "payment", "billing", "invoice",
	}
	// Common phishing brand names abused
	phishingBrands = []string{
		"paypal", "apple", "google", "microsoft", "facebook", "instagram",
		"netflix", "amazon", "dropbox", "whatsapp", "telegram", "alipay",
		"wechat", "qq", "taobao", "jd", "baidu", "sina", "163", "126",
	}
	// IP-based URLs are never legitimate shortlink targets
	ipURLPattern = regexp.MustCompile(`^https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	// Excessive hyphens / subdomains
	excessiveHyphens = regexp.MustCompile(`-{3,}`)
	manySubdomains   = regexp.MustCompile(`^https?://([^.]+\.){4,}`)
)

// Offensive keywords for URL checking (subset, loaded from word filter too)
var offensiveURLKeywords = []string{
	"porn", "sex", "xxx", "adult", "nude", "escort", "gambling", "casino",
	"bet", "lottery", "drug", "cannabis", "cocaine", "heroin", "viagra",
	"cialis", "dating", "hookup", "onlyfans", "chaturbate",
}

// ScanURL performs safe checks on a URL without making HTTP requests to the target.
func (s *SafeScanner) ScanURL(rawURL string) *ScanResult {
	result := &ScanResult{Safe: true}
	rawURL = strings.TrimSpace(rawURL)

	// Parse URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		result.Safe = false
		result.Risks = append(result.Risks, "invalid_url")
		result.Score = 100
		return result
	}

	host := strings.ToLower(parsed.Hostname())

	// 1. IP-based URL (always suspicious for shortlinks)
	if ipURLPattern.MatchString(rawURL) {
		result.Safe = false
		result.Risks = append(result.Risks, "ip_based_url")
		result.Score += 40
	}

	// 2. Check for excessive subdomains (phishing pattern)
	if manySubdomains.MatchString(rawURL) {
		result.Risks = append(result.Risks, "excessive_subdomains")
		result.Score += 20
	}

	// 3. Check for excessive hyphens
	if excessiveHyphens.MatchString(host) {
		result.Risks = append(result.Risks, "suspicious_hyphens")
		result.Score += 15
	}

	// 4. Suspicious TLD check
	tld := extractTLD(host)
	if suspiciousTLDs[tld] {
		result.Risks = append(result.Risks, "suspicious_tld:"+tld)
		result.Score += 25
	}

	// 5. Phishing keyword in domain
	for _, kw := range phishingKeywords {
		if strings.Contains(host, kw) {
			// Check if it's a legitimate use (e.g., login.legitcompany.com)
			brandFound := false
			for _, brand := range phishingBrands {
				if strings.Contains(host, brand) {
					brandFound = true
					break
				}
			}
			if brandFound {
				result.Risks = append(result.Risks, "potential_phishing:"+kw+"+"+host)
				result.Score += 30
			}
			break
		}
	}

	// 6. Offensive keywords
	for _, kw := range offensiveURLKeywords {
		if strings.Contains(host, kw) {
			result.Risks = append(result.Risks, "offensive_keyword:"+kw)
			result.Score += 35
			break
		}
	}

	// 7. DNS lookup (safe — just resolves, no connection to target server)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		// Domain doesn't resolve — might be brand new (phishing) or fake
		result.Risks = append(result.Risks, "domain_not_resolving")
		result.Score += 20
	} else {
		// Check if IP is private (someone linked to internal network).
		// Treat private resolution as a hard block for public/API-created links.
		for _, ip := range ips {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				result.Safe = false
				result.Risks = append(result.Risks, "internal_ip:"+ip.String())
				result.Score = 100
				break
			}
		}
	}

	// 8. URL length heuristic (very long URLs often obfuscated)
	if len(rawURL) > 500 {
		result.Risks = append(result.Risks, "excessive_url_length")
		result.Score += 10
	}

	if result.Score >= 50 {
		result.Safe = false
	}

	return result
}

func extractTLD(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (s *SafeScanner) FormatRisk(risks []string) string {
	return fmt.Sprintf("[SafeScanner] risks: %s", strings.Join(risks, ", "))
}

// CheckAgainstKeywords checks a string against a provided keyword list.
// Returns true if any keyword is found.
func CheckAgainstKeywords(text string, keywords []string) (bool, string) {
	text = strings.ToLower(text)
	for _, kw := range keywords {
		if len(kw) >= 2 && strings.Contains(text, strings.ToLower(kw)) {
			return true, kw
		}
	}
	return false, ""
}
