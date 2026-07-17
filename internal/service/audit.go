package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"shortlink/internal/model"
	"shortlink/internal/repository"

	"go.uber.org/zap"
)

type AuditService struct {
	repo *repository.AuditRepository
	log  *zap.Logger
}

func NewAuditService(repo *repository.AuditRepository, log *zap.Logger) *AuditService {
	return &AuditService{repo: repo, log: log}
}

func (s *AuditService) List(ctx context.Context, offset, limit int) ([]model.AdminAuditLog, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, nil
	}
	return s.repo.List(ctx, offset, limit)
}

func (s *AuditService) Record(ctx context.Context, actorType, actorID, action, resource, resourceID, ip, ua, metadata string) {
	if s == nil || s.repo == nil {
		return
	}
	entry := &model.AdminAuditLog{ActorType: actorType, ActorID: actorID, Action: action, Resource: resource, ResourceID: resourceID, IPHash: hashText(ip), UserAgentHash: hashText(ua), MetadataJSON: metadata}
	if err := s.repo.Create(ctx, entry); err != nil && s.log != nil {
		s.log.Warn("write audit log failed", zap.Error(err))
	}
}

func hashText(v string) string {
	if v == "" {
		return ""
	}
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}
