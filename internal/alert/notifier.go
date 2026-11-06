// Package alert evaluates alert rules against Monit status data and sends
// grouped notifications through pluggable channels.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Notification kinds.
const (
	KindFailure     = "failure"
	KindRecovery    = "recovery"
	KindHostOffline = "host_offline"
	KindHostOnline  = "host_online"
)

// Item is one service (or, for host_offline, the host itself) inside a Notification.
type Item struct {
	HostID   string
	Hostname string
	Service  string
	Type     int
	Message  string
	Since    time.Time
}

// Notification is one message to one channel. It carries every matching item
// of a rule across all hosts, so a cascade (a database crash and the sites
// that depend on it, on any hosts) produces one message.
type Notification struct {
	Kind  string
	Items []Item
	At    time.Time
}

func (n Notification) hostCount() int {
	hosts := map[string]bool{}
	for _, it := range n.Items {
		hosts[it.HostID] = true
	}
	return len(hosts)
}

func (n Notification) firstHostname() string {
	if len(n.Items) == 0 {
		return ""
	}
	return n.Items[0].Hostname
}

// Subject returns a one-line summary.
func (n Notification) Subject() string {
	hosts := n.hostCount()
	switch n.Kind {
	case KindHostOffline:
		if hosts == 1 {
			return fmt.Sprintf("[cmonit] %s is OFFLINE", n.firstHostname())
		}
		return fmt.Sprintf("[cmonit] %d hosts are OFFLINE", hosts)
	case KindHostOnline:
		if hosts == 1 {
			return fmt.Sprintf("[cmonit] %s is back online", n.firstHostname())
		}
		return fmt.Sprintf("[cmonit] %d hosts are back online", hosts)
	}
	verb := "failing"
	if n.Kind == KindRecovery {
		verb = "recovered"
	}
	if hosts == 1 && len(n.Items) == 1 {
		return fmt.Sprintf("[cmonit] %s: 1 service %s", n.firstHostname(), verb)
	}
	subject := fmt.Sprintf("[cmonit] %d services %s on %d host(s)", len(n.Items), verb, hosts)
	if n.Kind == KindFailure {
		subject += " (first: " + itemName(n.chronological()[0]) + ")"
	}
	return subject
}

// chronological returns the items oldest first; ties sort by host and service
// because Since has one-second resolution.
func (n Notification) chronological() []Item {
	items := append([]Item(nil), n.Items...)
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if !a.Since.Equal(b.Since) {
			return a.Since.Before(b.Since)
		}
		if a.Hostname != b.Hostname {
			return a.Hostname < b.Hostname
		}
		return a.Service < b.Service
	})
	return items
}

func itemName(it Item) string {
	if it.Service == "" {
		return it.Hostname
	}
	return it.Service + "@" + it.Hostname
}

// Body returns the plain-text body in chronological order. The first line of a
// cascade is a hint, not a diagnosis: the resolution is the agents' poll
// interval and agent clocks may differ.
func (n Notification) Body() string {
	items := n.chronological()
	var b strings.Builder
	b.WriteString(n.Subject())
	b.WriteString("\n\n")
	if len(items) > 0 {
		first := items[0].Since
		for i, it := range items {
			offset := "first"
			if i > 0 {
				offset = fmt.Sprintf("+%ds", int(it.Since.Sub(first).Seconds()))
			}
			fmt.Fprintf(&b, "%s  %-6s %s", it.Since.Format("15:04:05"), offset, itemName(it))
			if it.Message != "" {
				fmt.Fprintf(&b, ": %s", it.Message)
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\nTime: %s\n", n.At.Format(time.RFC3339))
	return b.String()
}

// Notifier delivers a Notification to one medium.
type Notifier interface {
	Send(ctx context.Context, n Notification) error
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// NewNotifier builds a Notifier from a channel type and its JSON config.
func NewNotifier(channelType string, config json.RawMessage) (Notifier, error) {
	switch channelType {
	case "email":
		var c emailConfig
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("invalid email config: %w", err)
		}
		if c.Host == "" || c.From == "" || len(c.To) == 0 {
			return nil, fmt.Errorf("email config requires host, from, to")
		}
		if c.Port == 0 {
			c.Port = 25
		}
		return &c, nil
	case "telegram":
		var c telegramConfig
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("invalid telegram config: %w", err)
		}
		if c.BotToken == "" || c.ChatID == "" {
			return nil, fmt.Errorf("telegram config requires bot_token, chat_id")
		}
		return &c, nil
	case "slack":
		var c slackConfig
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("invalid slack config: %w", err)
		}
		if c.WebhookURL == "" {
			return nil, fmt.Errorf("slack config requires webhook_url")
		}
		return &c, nil
	case "webhook":
		var c webhookConfig
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("invalid webhook config: %w", err)
		}
		if c.URL == "" {
			return nil, fmt.Errorf("webhook config requires url")
		}
		return &c, nil
	default:
		return nil, fmt.Errorf("unknown channel type %q (supported: email, telegram, slack, webhook)", channelType)
	}
}

func postJSON(ctx context.Context, target string, headers map[string]string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return redactURLError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// redactURLError drops the request URL from transport errors: Telegram and
// Slack embed their secret token in the URL and the error ends up in alert_log.
func redactURLError(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

type emailConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

// Send uses net/smtp.SendMail, which upgrades to STARTTLS when the server
// offers it. Implicit TLS (port 465) is not supported.
func (c *emailConfig) Send(ctx context.Context, n Notification) error {
	var auth smtp.Auth
	if c.Username != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
	}
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace(n.Subject())
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		c.From, strings.Join(c.To, ", "), subject, n.At.Format(time.RFC1123Z), n.Body())

	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(fmt.Sprintf("%s:%d", c.Host, c.Port), auth, c.From, c.To, []byte(msg))
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

func (c *telegramConfig) Send(ctx context.Context, n Notification) error {
	target := "https://api.telegram.org/bot" + c.BotToken + "/sendMessage"
	return postJSON(ctx, target, nil, map[string]string{"chat_id": c.ChatID, "text": n.Body()})
}

type slackConfig struct {
	WebhookURL string `json:"webhook_url"`
}

// Send posts to a Slack incoming webhook.
func (c *slackConfig) Send(ctx context.Context, n Notification) error {
	return postJSON(ctx, c.WebhookURL, nil, map[string]string{"text": n.Body()})
}

type webhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type webhookItem struct {
	HostID   string `json:"host_id"`
	Hostname string `json:"hostname"`
	Service  string `json:"service"`
	Type     int    `json:"type"`
	Message  string `json:"message"`
	Since    int64  `json:"since"`
}

type webhookPayload struct {
	Kind    string        `json:"kind"`
	Subject string        `json:"subject"`
	Time    int64         `json:"time"`
	Items   []webhookItem `json:"items"`
}

// Send posts a structured JSON payload; this is the integration point for
// anything without a dedicated notifier (ntfy, Discord, Teams, WhatsApp via Twilio, ...).
func (c *webhookConfig) Send(ctx context.Context, n Notification) error {
	p := webhookPayload{Kind: n.Kind, Subject: n.Subject(), Time: n.At.Unix(), Items: make([]webhookItem, 0, len(n.Items))}
	for _, it := range n.Items {
		p.Items = append(p.Items, webhookItem{HostID: it.HostID, Hostname: it.Hostname, Service: it.Service, Type: it.Type, Message: it.Message, Since: it.Since.Unix()})
	}
	return postJSON(ctx, c.URL, c.Headers, p)
}
