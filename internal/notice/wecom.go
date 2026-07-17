package notice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type WeComNotifier struct {
	webhook string
}

func NewWeComNotifier(webhook string) *WeComNotifier {
	return &WeComNotifier{webhook: webhook}
}

func (w *WeComNotifier) Name() string { return "wecom" }

func (w *WeComNotifier) Send(ctx context.Context, message string) error {
	payload := map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": message},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", w.webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wecom webhook status: %d", resp.StatusCode)
	}
	return nil
}
