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

type DingtalkNotifier struct {
	webhook string
	secret  string
}

func NewDingtalkNotifier(webhook, secret string) *DingtalkNotifier {
	return &DingtalkNotifier{webhook: webhook, secret: secret}
}

func (d *DingtalkNotifier) Name() string {
	return "dingtalk"
}

func (d *DingtalkNotifier) Send(ctx context.Context, message string) error {
	timestamp := time.Now().UnixMilli()
	sign := d.genSign(timestamp)

	url := fmt.Sprintf("%s&timestamp=%d&sign=%s", d.webhook, timestamp, sign)
	payload := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]string{
			"content": message,
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
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
		return fmt.Errorf("dingtalk webhook status: %d", resp.StatusCode)
	}
	return nil
}

func (d *DingtalkNotifier) genSign(timestamp int64) string {
	if d.secret == "" {
		return ""
	}
	str := fmt.Sprintf("%d\n%s", timestamp, d.secret)
	h := hmac.New(sha256.New, []byte(d.secret))
	h.Write([]byte(str))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}
