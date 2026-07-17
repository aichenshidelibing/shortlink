package service

import (
	"context"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"strings"
)

type DomainService struct{ repo *repository.DomainRepository }

func NewDomainService(repo *repository.DomainRepository) *DomainService {
	return &DomainService{repo: repo}
}
func (s *DomainService) List(ctx context.Context) ([]model.Domain, error) { return s.repo.List(ctx) }
func (s *DomainService) Create(ctx context.Context, d *model.Domain) error {
	d.Hostname = strings.ToLower(strings.TrimSpace(d.Hostname))
	if d.Purpose == "" {
		d.Purpose = "public"
	}
	d.Enabled = true
	return s.repo.Create(ctx, d)
}
func (s *DomainService) Update(ctx context.Context, id uint64, d *model.Domain) error {
	return s.repo.Update(ctx, id, map[string]interface{}{"hostname": strings.ToLower(strings.TrimSpace(d.Hostname)), "purpose": d.Purpose, "is_default": d.IsDefault, "force_https": d.ForceHTTPS, "enabled": d.Enabled})
}
func (s *DomainService) Delete(ctx context.Context, id uint64) error { return s.repo.Delete(ctx, id) }
