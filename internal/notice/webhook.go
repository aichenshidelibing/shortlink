package notice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// WebhookNotifier posts a JSON body {"text": "<message>"} to any HTTP endpoint.
type WebhookNotifier struct {
	url    string
	secret string // sent as X-Webhook-Secret header if non-empty
}

func NewWebhookNotifier(webhookURL, secret string) *WebhookNotifier {
	return &WebhookNotifier{url: webhookURL, secret: secret}
}

func (w *WebhookNotifier) Name() string { return "webhook" }

func (w *WebhookNotifier) Send(ctx context.Context, message string) error {
	payload := map[string]string{"text": message}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.secret != "" {
		req.Header.Set("X-Webhook-Secret", w.secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status: %d", resp.StatusCode)
	}
	return nil
}
