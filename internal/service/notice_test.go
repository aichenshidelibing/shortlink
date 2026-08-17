package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shortlink/internal/config"
)

func TestNoticeServiceReloadAndSendTest(t *testing.T) {
	gotMessage := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode webhook payload: %v", err)
			return
		}
		gotMessage <- payload.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s := NewNoticeService(&config.NotificationConfig{
		Webhook: config.WebhookConfig{Enabled: true, URL: ts.URL, Secret: "test-secret"},
	}, nil)
	if err := s.SendTest(context.Background(), "webhook"); err != nil {
		t.Fatalf("send notification test: %v", err)
	}
	select {
	case message := <-gotMessage:
		if !strings.Contains(message, "通知渠道测试") {
			t.Fatalf("unexpected test message: %q", message)
		}
	default:
		t.Fatal("webhook did not receive test message")
	}

	s.ReloadFromJSON(`{"webhook_secret":"updated"}`)
	cfg := s.Config()
	if cfg.Webhook.URL != ts.URL || cfg.Webhook.Secret != "updated" || !cfg.Webhook.Enabled {
		t.Fatalf("partial reload lost existing webhook config: %+v", cfg.Webhook)
	}
}

func TestNoticeServiceSendTestRejectsUnknownOrDisabled(t *testing.T) {
	s := NewNoticeService(nil, nil)
	if err := s.SendTest(context.Background(), "unknown"); err == nil {
		t.Fatal("expected unknown provider error")
	}
	if err := s.SendTest(context.Background(), "webhook"); err == nil {
		t.Fatal("expected disabled provider error")
	}
}
