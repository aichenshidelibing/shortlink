package notice

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type FeishuNotifier struct {
	webhook string
	secret  string
}

func NewFeishuNotifier(webhook, secret string) *FeishuNotifier {
	return &FeishuNotifier{webhook: webhook, secret: secret}
}

func (f *FeishuNotifier) Name() string {
	return "feishu"
}

func (f *FeishuNotifier) Send(ctx context.Context, message string) error {
	timestamp := time.Now().Unix()
	sign := f.genSign(timestamp)

	payload := map[string]interface{}{
		"timestamp": timestamp,
		"sign":      sign,
		"msg_type":  "text",
		"content": map[string]string{
			"text": message,
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", f.webhook, bytes.NewReader(body))
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
		return fmt.Errorf("feishu webhook status: %d", resp.StatusCode)
	}
	return nil
}

func (f *FeishuNotifier) genSign(timestamp int64) string {
	if f.secret == "" {
		return ""
	}
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, f.secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	h.Write(nil)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
