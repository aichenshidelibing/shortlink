package service

import (
	"context"
	"encoding/json"
	"fmt"
	"shortlink/internal/auth"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"time"

	"go.uber.org/zap"
)

type APIKeyService struct {
	repo  *repository.APIKeyRepository
	cache *repository.CacheRepository
	log   *zap.Logger
}

func NewAPIKeyService(repo *repository.APIKeyRepository, log *zap.Logger, cache ...*repository.CacheRepository) *APIKeyService {
	s := &APIKeyService{repo: repo, log: log}
	if len(cache) > 0 {
		s.cache = cache[0]
	}
	return s
}

type APIKeyOptions struct {
	Name           string
	Purpose        string
	Permissions    []string
	QuotaPerMinute int
	QuotaPerDay    int
	QuotaPerMonth  int
	AllowedDomains string
	DeniedDomains  string
	ExpiresAt      *time.Time
}

func (s *APIKeyService) Create(ctx context.Context, name string, expiresAt *time.Time) (string, *model.APIKey, error) {
	return s.CreateWithOptions(ctx, APIKeyOptions{Name: name, ExpiresAt: expiresAt, Permissions: []string{"links:create"}})
}

func (s *APIKeyService) CreateWithOptions(ctx context.Context, opts APIKeyOptions) (string, *model.APIKey, error) {
	plainKey := auth.GenerateAPIKey()
	hash := auth.HashAPIKey(plainKey)
	perms := opts.Permissions
	if len(perms) == 0 {
		perms = []string{"links:create"}
	}
	buf, _ := json.Marshal(perms)

	key := &model.APIKey{
		KeyHash:         hash,
		Name:            opts.Name,
		Purpose:         opts.Purpose,
		PermissionsJSON: string(buf),
		QuotaPerMinute:  opts.QuotaPerMinute,
		QuotaPerDay:     opts.QuotaPerDay,
		QuotaPerMonth:   opts.QuotaPerMonth,
		AllowedDomains:  opts.AllowedDomains,
		DeniedDomains:   opts.DeniedDomains,
		ExpiresAt:       opts.ExpiresAt,
	}
	if err := s.repo.Create(ctx, key); err != nil {
		return "", nil, err
	}

	return plainKey, key, nil
}

func (s *APIKeyService) Validate(ctx context.Context, plainKey string) (*model.APIKey, error) {
	if plainKey == "" {
		return nil, fmt.Errorf("api key is empty")
	}
	hash := auth.HashAPIKey(plainKey)
	key, err := s.repo.GetByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("invalid api key")
	}

	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, fmt.Errorf("api key expired")
	}

	if key.Revoked {
		return nil, fmt.Errorf("api key revoked")
	}

	s.repo.UpdateLastUsed(ctx, key.ID)
	return key, nil
}

func (s *APIKeyService) CheckQuota(ctx context.Context, key *model.APIKey, cost int) error {
	if key == nil || s.cache == nil || cost <= 0 {
		return nil
	}
	now := time.Now()
	checks := []struct {
		limit  int
		key    string
		window time.Duration
	}{
		{key.QuotaPerMinute, fmt.Sprintf("apikey:%d:quota:minute:%s", key.ID, now.Format("200601021504")), time.Minute + 5*time.Second},
		{key.QuotaPerDay, fmt.Sprintf("apikey:%d:quota:day:%s", key.ID, now.Format("20060102")), 48 * time.Hour},
		{key.QuotaPerMonth, fmt.Sprintf("apikey:%d:quota:month:%s", key.ID, now.Format("200601")), 32 * 24 * time.Hour},
	}
	for _, chk := range checks {
		if chk.limit <= 0 {
			continue
		}
		for i := 0; i < cost; i++ {
			ok, err := s.cache.RateLimitCheck(ctx, chk.key, chk.limit, chk.window)
			if err != nil {
				return fmt.Errorf("quota check failed")
			}
			if !ok {
				return fmt.Errorf("api key quota exceeded")
			}
		}
	}
	return nil
}

func (s *APIKeyService) HasPermission(key *model.APIKey, perm string) bool {
	if key == nil {
		return false
	}
	var perms []string
	_ = json.Unmarshal([]byte(key.PermissionsJSON), &perms)
	if len(perms) == 0 && perm == "links:create" {
		return true
	}
	for _, p := range perms {
		if p == perm || p == "*" {
			return true
		}
	}
	return false
}

func (s *APIKeyService) CheckDomainAllowed(key *model.APIKey, host string) error {
	if key == nil {
		return nil
	}
	if MatchDomainList(host, key.DeniedDomains) {
		return fmt.Errorf("api key is not allowed to create links for this domain")
	}
	if DomainListHasEntries(key.AllowedDomains) && !MatchDomainList(host, key.AllowedDomains) {
		return fmt.Errorf("api key is not allowed to create links for this domain")
	}
	return nil
}

func (s *APIKeyService) GetByID(ctx context.Context, id uint64) (*model.APIKey, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("api key service unavailable")
	}
	key, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("api key not found")
	}
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, fmt.Errorf("api key expired")
	}
	if key.Revoked {
		return nil, fmt.Errorf("api key revoked")
	}
	return key, nil
}

func (s *APIKeyService) List(ctx context.Context) ([]model.APIKey, error) {
	return s.repo.List(ctx)
}

func (s *APIKeyService) Update(ctx context.Context, id uint64, opts APIKeyOptions) error {
	values := map[string]interface{}{
		"name":             opts.Name,
		"purpose":          opts.Purpose,
		"quota_per_minute": opts.QuotaPerMinute,
		"quota_per_day":    opts.QuotaPerDay,
		"quota_per_month":  opts.QuotaPerMonth,
		"allowed_domains":  opts.AllowedDomains,
		"denied_domains":   opts.DeniedDomains,
		"expires_at":       opts.ExpiresAt,
	}
	if opts.Permissions != nil {
		buf, _ := json.Marshal(opts.Permissions)
		values["permissions_json"] = string(buf)
	}
	return s.repo.Update(ctx, id, values)
}

func (s *APIKeyService) Revoke(ctx context.Context, id uint64) error {
	return s.repo.Revoke(ctx, id)
}

func (s *APIKeyService) Delete(ctx context.Context, id uint64) error {
	return s.repo.Delete(ctx, id)
}
