package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ModerationService wraps optional AI moderation.
type ModerationService struct {
	openAIKey string
	enabled   bool
	client    *http.Client
	log       *zap.Logger
}

type ModerationConfig struct {
	Enabled    bool   `json:"mod_enabled"`
	OpenAIKey  string `json:"mod_openai_key"`
	AutoDelete bool   `json:"mod_auto_delete"` // auto-delete on high confidence flag
}

func NewModerationService(cfg *ModerationConfig, log *zap.Logger) *ModerationService {
	s := &ModerationService{
		enabled: cfg.Enabled && cfg.OpenAIKey != "",
		client:  &http.Client{Timeout: 10 * time.Second},
		log:     log,
	}
	if s.enabled {
		s.openAIKey = cfg.OpenAIKey
	}
	return s
}

// ReloadConfig updates moderation settings at runtime.
func (s *ModerationService) ReloadConfig(cfg *ModerationConfig) {
	s.enabled = cfg.Enabled && cfg.OpenAIKey != ""
	s.openAIKey = cfg.OpenAIKey
}

type moderationReq struct {
	Input string `json:"input"`
	Model string `json:"model,omitempty"`
}

type moderationCategory struct {
	Category string  `json:"category"`
	Score    float64 `json:"score"`
}

type moderationResp struct {
	Results []struct {
		Flagged    bool               `json:"flagged"`
		Categories []moderationCategory `json:"categories"`
	} `json:"results"`
}

// CheckURL sends a URL to OpenAI moderation and returns whether it's flagged.
// Returns: (flagged bool, reason string)
func (s *ModerationService) CheckURL(ctx context.Context, url string) (bool, string) {
	if !s.enabled {
		return false, ""
	}

	reqBody := moderationReq{Input: url}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/moderations", bytes.NewReader(body))
	if err != nil {
		s.log.Warn("moderation request failed", zap.Error(err))
		return false, ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.openAIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("moderation api error", zap.Error(err))
		return false, ""
	}
	defer resp.Body.Close()

	var result moderationResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, ""
	}

	if len(result.Results) > 0 && result.Results[0].Flagged {
		var reasons []string
		for _, cat := range result.Results[0].Categories {
			if cat.Score > 0.5 {
				reasons = append(reasons, fmt.Sprintf("%s(%.0f%%)", cat.Category, cat.Score*100))
			}
		}
		return true, strings.Join(reasons, ", ")
	}
	return false, ""
}

// CheckReportText checks report custom text through moderation.
func (s *ModerationService) CheckReportText(ctx context.Context, text string) (bool, string) {
	if !s.enabled || text == "" {
		return false, ""
	}
	return s.CheckURL(ctx, text)
}
