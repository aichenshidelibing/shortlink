package service

import (
	"shortlink/internal/model"
	"testing"
	"time"
)

func TestBuildAvailabilityPeriodUsesRealSamples(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	samples := []model.AvailabilitySample{
		{CheckedAt: now.Add(-50 * time.Minute), Available: true, LatencyMs: 50},
		{CheckedAt: now.Add(-40 * time.Minute), Available: true, LatencyMs: 60},
		{CheckedAt: now.Add(-30 * time.Minute), Available: false, LatencyMs: 80},
		{CheckedAt: now.Add(-20 * time.Minute), Available: true, LatencyMs: 700},
	}
	period := buildAvailabilityPeriod("1h", now.Add(-time.Hour), now, samples)
	if period.Samples != 4 {
		t.Fatalf("samples=%d want 4", period.Samples)
	}
	if period.UptimePercent != 75.0 {
		t.Fatalf("uptime=%v want 75.0", period.UptimePercent)
	}
	if period.LatencyMs != 700 {
		t.Fatalf("latency=%d want latest latency 700", period.LatencyMs)
	}
	unknown := false
	for _, bar := range period.Bars {
		if bar == "unknown" {
			unknown = true
			break
		}
	}
	if !unknown {
		t.Fatal("expected sparse history to produce unknown bars")
	}
}

func TestClassifyAvailabilityBucket(t *testing.T) {
	cases := []struct {
		name    string
		samples []model.AvailabilitySample
		want    string
	}{
		{name: "unknown", samples: nil, want: "unknown"},
		{name: "ok", samples: []model.AvailabilitySample{{Available: true, LatencyMs: 100}}, want: "ok"},
		{name: "slow latency", samples: []model.AvailabilitySample{{Available: true, LatencyMs: 400}}, want: "slow"},
		{name: "partial", samples: []model.AvailabilitySample{{Available: true, LatencyMs: 100}, {Available: false, LatencyMs: 100}}, want: "slow"},
		{name: "down", samples: []model.AvailabilitySample{{Available: false, LatencyMs: 100}}, want: "down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyAvailabilityBucket(tc.samples); got != tc.want {
				t.Fatalf("classifyAvailabilityBucket()=%q want %q", got, tc.want)
			}
		})
	}
}
