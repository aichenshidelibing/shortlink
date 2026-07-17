package notice

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

type EmailNotifier struct {
	host     string
	port     string
	username string
	password string
	from     string
	to       string // comma-separated recipients
}

func NewEmailNotifier(host, port, username, password, from, to string) *EmailNotifier {
	if port == "" {
		port = "587"
	}
	return &EmailNotifier{host: host, port: port, username: username, password: password, from: from, to: to}
}

func (e *EmailNotifier) Name() string { return "email" }

func (e *EmailNotifier) Send(_ context.Context, message string) error {
	addr := e.host + ":" + e.port
	auth := smtp.PlainAuth("", e.username, e.password, e.host)

	recipients := strings.Split(e.to, ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}

	subject := "【Shortlink】系统通知"
	body := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		strings.Join(recipients, ","), e.from, subject, message)

	if err := smtp.SendMail(addr, auth, e.from, recipients, []byte(body)); err != nil {
		return fmt.Errorf("email send: %w", err)
	}
	return nil
}
