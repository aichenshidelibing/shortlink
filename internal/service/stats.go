package service

import (
	"context"
	"fmt"
	"shortlink/internal/model"
	"shortlink/internal/pkg/geoip"
	"shortlink/internal/repository"
	"time"

	"go.uber.org/zap"
)

type StatsService struct {
	clickRepo *repository.ClickRepository
	linkRepo  *repository.LinkRepository
	log       *zap.Logger
}

func NewStatsService(clickRepo *repository.ClickRepository, linkRepo *repository.LinkRepository, log *zap.Logger) *StatsService {
	return &StatsService{
		clickRepo: clickRepo,
		linkRepo:  linkRepo,
		log:       log,
	}
}

func (s *StatsService) RecordClick(ctx context.Context, linkID uint64, ip string) (*model.RealtimeClick, error) {
	geo, err := geoip.Lookup(ip)
	if err != nil {
		geo = &geoip.GeoInfo{Country: "Unknown", Region: "Unknown"}
	}

	click := &model.RealtimeClick{
		Country: geo.Country,
		Region:  geo.Region,
		Time:    time.Now(),
	}

	return click, nil
}

func (s *StatsService) GetLinkStats(ctx context.Context, code string) (*model.LinkStats, error) {
	link, err := s.linkRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("link not found")
	}

	timeline, geo, err := s.clickRepo.GetStats(ctx, link.ID, 7)
	if err != nil {
		return nil, err
	}

	unique, _ := s.clickRepo.CountUniqueVisitors(ctx, link.ID, 7)
	refs, _ := s.clickRepo.GroupByField(ctx, link.ID, 7, "referer_host")
	devices, _ := s.clickRepo.GroupByField(ctx, link.ID, 7, "device")
	browsers, _ := s.clickRepo.GroupByField(ctx, link.ID, 7, "browser")
	oses, _ := s.clickRepo.GroupByField(ctx, link.ID, 7, "os")
	hours, _ := s.clickRepo.GroupByHour(ctx, link.ID, 7)
	return &model.LinkStats{
		ShortCode:       link.ShortCode,
		TotalClicks:     link.ClickCount,
		UniqueCountries: len(geo),
		UniqueVisitors:  unique,
		Timeline:        timeline,
		Geo:             geo,
		Referrers:       refs,
		Devices:         devices,
		Browsers:        browsers,
		OS:              oses,
		Hours:           hours,
	}, nil
}

func (s *StatsService) GetGeoStats(ctx context.Context, code string) ([]model.GeoStat, error) {
	link, err := s.linkRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	_, geo, err := s.clickRepo.GetStats(ctx, link.ID, 7)
	return geo, err
}

func (s *StatsService) GetDashboardStats(ctx context.Context) (map[string]interface{}, error) {
	_, totalLinks, err := s.linkRepo.List(ctx, 0, 1)
	if err != nil {
		totalLinks = 0
	}

	return map[string]interface{}{
		"total_links":  totalLinks,
		"total_clicks": 0,
	}, nil
}
