package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"shortlink/internal/crypto"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"time"

	"go.uber.org/zap"
)

type BanService struct {
	repo   *repository.BanRepository
	strong *crypto.StrongCrypto
	log    *zap.Logger
}

func NewBanService(repo *repository.BanRepository, strong *crypto.StrongCrypto, log *zap.Logger) *BanService {
	return &BanService{repo: repo, strong: strong, log: log}
}

func hashIP(ip string) string {
	h := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(h[:])
}

func (s *BanService) BanIP(ctx context.Context, ip, reason string, duration time.Duration) error {
	ipHash := hashIP(ip)

	// Skip if already banned
	banned, _, err := s.IsBanned(ctx, ip)
	if err != nil {
		return err
	}
	if banned {
		return nil
	}

	ipEnc, nonce, err := s.strong.Encrypt([]byte(ip))
	if err != nil {
		return err
	}

	ban := &model.BannedIP{
		IPHash:   ipHash,
		IPEnc:    ipEnc,
		IPNonce:  nonce,
		Reason:   reason,
		BanUntil: time.Now().Add(duration),
	}
	return s.repo.Create(ctx, ban)
}

func (s *BanService) IsBanned(ctx context.Context, ip string) (bool, string, error) {
	ipHash := hashIP(ip)
	banned, err := s.repo.IsBanned(ctx, ipHash)
	if err != nil {
		return false, "", err
	}
	if banned {
		return true, "ip is banned", nil
	}
	return false, "", nil
}

func (s *BanService) List(ctx context.Context) ([]model.BannedIP, error) {
	return s.repo.List(ctx)
}

func (s *BanService) CountActive(ctx context.Context) (int64, error) {
	return s.repo.CountActive(ctx)
}

func (s *BanService) Unban(ctx context.Context, id uint64) error {
	return s.repo.Delete(ctx, id)
}

func (s *BanService) Cleanup(ctx context.Context) error {
	return s.repo.CleanupExpired(ctx)
}

func (s *BanService) IsIPInCIDR(ipStr, cidrStr string) bool {
	ip := net.ParseIP(ipStr)
	_, ipNet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return false
	}
	return ipNet.Contains(ip)
}
