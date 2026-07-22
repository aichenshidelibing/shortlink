package service

import (
	"context"
	"math"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"time"

	"go.uber.org/zap"
)

type AvailabilityPeriod struct {
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	Available      bool     `json:"available"`
	Status         string   `json:"status"`
	LatencyMs      int      `json:"latency_ms"`
	UptimePercent  float64  `json:"uptime_percent"`
	LastCheckedMin int      `json:"last_checked_min"`
	Samples        int      `json:"samples"`
	Bars           []string `json:"bars"`
}

type PublicAvailabilitySummary struct {
	Available bool   `json:"available"`
	Status    string `json:"status"`
	LatencyMs int    `json:"latency_ms"`
}

type PublicAvailability struct {
	Service string                    `json:"service"`
	Summary PublicAvailabilitySummary `json:"summary"`
	Periods []AvailabilityPeriod      `json:"periods"`
}

type StatusService struct {
	repo     *repository.StatusRepository
	adminSvc *AdminService
	log      *zap.Logger
}

func NewStatusService(repo *repository.StatusRepository, adminSvc *AdminService, log *zap.Logger) *StatusService {
	return &StatusService{repo: repo, adminSvc: adminSvc, log: log}
}

func (s *StatusService) CheckOnce(ctx context.Context, now time.Time) (*model.AvailabilitySample, error) {
	started := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	dbOK := false
	if s.adminSvc != nil && s.adminSvc.GetDB() != nil {
		dbOK = s.adminSvc.GetDB().WithContext(checkCtx).Raw("SELECT 1").Error == nil
	}
	redisOK := false
	if s.adminSvc != nil {
		redisOK, _ = s.adminSvc.CheckRedis(checkCtx)
	}
	latencyMs := int(time.Since(started).Milliseconds())
	if latencyMs < 1 {
		latencyMs = 1
	}
	sample := &model.AvailabilitySample{
		CheckedAt: now,
		Available: dbOK && redisOK,
		DBOK:      dbOK,
		RedisOK:   redisOK,
		LatencyMs: latencyMs,
	}
	if s.repo == nil {
		return sample, nil
	}
	if err := s.repo.Create(ctx, sample); err != nil {
		return nil, err
	}
	return sample, nil
}

func (s *StatusService) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		_, _ = s.CheckOnce(ctx, time.Now())
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		cleanupTicker := time.NewTicker(time.Hour)
		defer cleanupTicker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := s.CheckOnce(ctx, time.Now()); err != nil && s.log != nil {
					s.log.Warn("availability sample failed", zap.Error(err))
				}
			case <-cleanupTicker.C:
				if s.repo != nil {
					_ = s.repo.DeleteBefore(ctx, time.Now().Add(-31*24*time.Hour))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *StatusService) PublicStatus(ctx context.Context, now time.Time) (*PublicAvailability, error) {
	var samples []model.AvailabilitySample
	var err error
	if s.repo != nil {
		samples, err = s.repo.ListSince(ctx, now.Add(-30*24*time.Hour))
		if err != nil {
			return nil, err
		}
	}
	if len(samples) == 0 {
		sample, err := s.CheckOnce(ctx, now)
		if err != nil {
			return nil, err
		}
		samples = []model.AvailabilitySample{*sample}
	}
	latest := latestSample(samples)
	out := &PublicAvailability{
		Service: "Shortlink",
		Summary: PublicAvailabilitySummary{
			Available: latest.Available,
			Status:    availabilityStatus(latest.Available),
			LatencyMs: latest.LatencyMs,
		},
	}
	out.Periods = []AvailabilityPeriod{
		buildAvailabilityPeriod("1h", now.Add(-time.Hour), now, samples),
		buildAvailabilityPeriod("24h", now.Add(-24*time.Hour), now, samples),
		buildAvailabilityPeriod("30d", now.Add(-30*24*time.Hour), now, samples),
	}
	return out, nil
}

func latestSample(samples []model.AvailabilitySample) model.AvailabilitySample {
	latest := samples[0]
	for _, sample := range samples[1:] {
		if sample.CheckedAt.After(latest.CheckedAt) {
			latest = sample
		}
	}
	return latest
}

func buildAvailabilityPeriod(key string, since, now time.Time, all []model.AvailabilitySample) AvailabilityPeriod {
	samples := make([]model.AvailabilitySample, 0)
	for _, sample := range all {
		if !sample.CheckedAt.Before(since) && !sample.CheckedAt.After(now) {
			samples = append(samples, sample)
		}
	}
	period := AvailabilityPeriod{Key: key, Label: key, Bars: buildAvailabilityBars(since, now, samples)}
	if len(samples) == 0 {
		period.Status = "unknown"
		period.UptimePercent = 0
		return period
	}
	latest := latestSample(samples)
	period.Available = latest.Available
	period.Status = availabilityStatus(latest.Available)
	period.LatencyMs = latest.LatencyMs
	period.LastCheckedMin = int(now.Sub(latest.CheckedAt).Minutes())
	if period.LastCheckedMin < 0 {
		period.LastCheckedMin = 0
	}
	period.Samples = len(samples)
	ok := 0
	for _, sample := range samples {
		if sample.Available {
			ok++
		}
	}
	period.UptimePercent = math.Round(float64(ok)/float64(len(samples))*1000) / 10
	return period
}

func buildAvailabilityBars(since, now time.Time, samples []model.AvailabilitySample) []string {
	const barCount = 60
	bars := make([]string, barCount)
	for i := range bars {
		bars[i] = "unknown"
	}
	span := now.Sub(since)
	if span <= 0 {
		return bars
	}
	bucket := span / barCount
	if bucket <= 0 {
		bucket = time.Minute
	}
	for i := range bars {
		bucketStart := since.Add(time.Duration(i) * bucket)
		bucketEnd := bucketStart.Add(bucket)
		bucketSamples := make([]model.AvailabilitySample, 0)
		for _, sample := range samples {
			if !sample.CheckedAt.Before(bucketStart) && sample.CheckedAt.Before(bucketEnd) {
				bucketSamples = append(bucketSamples, sample)
			}
		}
		bars[i] = classifyAvailabilityBucket(bucketSamples)
	}
	return bars
}

func classifyAvailabilityBucket(samples []model.AvailabilitySample) string {
	if len(samples) == 0 {
		return "unknown"
	}
	available := 0
	latencyTotal := 0
	for _, sample := range samples {
		if sample.Available {
			available++
		}
		latencyTotal += sample.LatencyMs
	}
	if available == 0 {
		return "down"
	}
	if available < len(samples) {
		return "slow"
	}
	if latencyTotal/len(samples) > 300 {
		return "slow"
	}
	return "ok"
}

func availabilityStatus(available bool) string {
	if available {
		return "online"
	}
	return "degraded"
}
