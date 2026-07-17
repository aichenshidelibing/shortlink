package notice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type DiscordNotifier struct {
	webhook string
}

func NewDiscordNotifier(webhook string) *DiscordNotifier {
	return &DiscordNotifier{webhook: webhook}
}

func (d *DiscordNotifier) Name() string { return "discord" }

func (d *DiscordNotifier) Send(ctx context.Context, message string) error {
	payload := map[string]string{"content": message}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", d.webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook status: %d", resp.StatusCode)
	}
	return nil
}
