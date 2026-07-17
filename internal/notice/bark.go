package notice

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type BarkNotifier struct {
	key      string
	endpoint string // default: https://api.day.app
}

func NewBarkNotifier(key, endpoint string) *BarkNotifier {
	ep := strings.TrimRight(endpoint, "/")
	if ep == "" {
		ep = "https://api.day.app"
	}
	return &BarkNotifier{key: key, endpoint: ep}
}

func (b *BarkNotifier) Name() string { return "bark" }

func (b *BarkNotifier) Send(ctx context.Context, message string) error {
	u := fmt.Sprintf("%s/%s/%s", b.endpoint, url.PathEscape(b.key), url.PathEscape(message))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bark status: %d", resp.StatusCode)
	}
	return nil
}
