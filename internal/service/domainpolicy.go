package service

import (
	"strings"
)

type DomainPolicyConfig struct {
	Allowlist           string
	Blocklist           string
	InterstitialEnabled bool
	WarnThreshold       int
	BlockThreshold      int
}

type DomainDecision struct {
	Blocked bool
	Warn    bool
	Score   int
	Level   string
	Reasons []string
}

func EvaluateDomainPolicy(host string, scan *ScanResult, cfg DomainPolicyConfig) DomainDecision {
	host = normalizeDomainHost(host)
	decision := DomainDecision{Level: "safe"}
	if MatchDomainList(host, cfg.Allowlist) {
		return decision
	}
	if MatchDomainList(host, cfg.Blocklist) {
		decision.Blocked = true
		decision.Score = 100
		decision.Level = "block"
		decision.Reasons = append(decision.Reasons, "domain_blocklist")
		return decision
	}
	if scan != nil {
		decision.Score = scan.Score
		decision.Reasons = append(decision.Reasons, scan.Risks...)
	}
	blockThreshold := cfg.BlockThreshold
	if blockThreshold <= 0 {
		blockThreshold = 85
	}
	warnThreshold := cfg.WarnThreshold
	if warnThreshold <= 0 {
		warnThreshold = 50
	}
	if decision.Score >= blockThreshold {
		decision.Blocked = true
		decision.Level = "block"
	} else if cfg.InterstitialEnabled && decision.Score >= warnThreshold {
		decision.Warn = true
		decision.Level = "warn"
	} else {
		decision.Level = "safe"
	}
	return decision
}

func normalizeDomainHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func splitDomainList(list string) []string {
	parts := strings.FieldsFunc(list, func(r rune) bool { return r == '\n' || r == ',' })
	patterns := make([]string, 0, len(parts))
	for _, part := range parts {
		pat := strings.ToLower(strings.TrimSpace(part))
		pat = strings.TrimPrefix(pat, ".")
		if pat == "" || strings.HasPrefix(pat, "#") {
			continue
		}
		patterns = append(patterns, pat)
	}
	return patterns
}

func DomainListHasEntries(list string) bool {
	return len(splitDomainList(list)) > 0
}

func MatchDomainList(host, list string) bool {
	host = normalizeDomainHost(host)
	for _, pat := range splitDomainList(list) {
		if strings.HasPrefix(pat, "*.") {
			base := strings.TrimPrefix(pat, "*.")
			if host == base || strings.HasSuffix(host, "."+base) {
				return true
			}
			continue
		}
		if host == pat || strings.HasSuffix(host, "."+pat) {
			return true
		}
	}
	return false
}
