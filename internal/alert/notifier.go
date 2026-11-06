// Package alert evaluates alert rules against Monit status data and sends
// grouped notifications through pluggable channels.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
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
	case "discord":
		var c discordConfig
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("invalid discord config: %w", err)
		}
		if c.WebhookURL == "" {
			return nil, fmt.Errorf("discord config requires webhook_url")
		}
		return &c, nil
	case "ntfy":
		var c ntfyConfig
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, fmt.Errorf("invalid ntfy config: %w", err)
		}
		if c.Server == "" {
			c.Server = "https://ntfy.sh"
		}
		if !ntfyTopicRe.MatchString(c.Topic) {
			return nil, fmt.Errorf("ntfy config requires topic (letters, digits, - and _, up to 64 characters)")
		}
		return &c, nil
	default:
		return nil, fmt.Errorf("unknown channel type %q (supported: email, telegram, slack, discord, ntfy, webhook)", channelType)
	}
}

func postJSON(ctx context.Context, target string, headers map[string]string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	return doPost(ctx, target, h, body, nil)
}

// doPost sends one POST and treats any non-2xx status as an error. setup may
// adjust the request (for example to add auth) before it is sent.
func doPost(ctx context.Context, target string, headers map[string]string, body []byte, setup func(*http.Request)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if setup != nil {
		setup(req)
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

// Send posts cmonit's own JSON payload. Targets that expect a specific format
// (Teams, Twilio, ...) need a relay that translates it.
func (c *webhookConfig) Send(ctx context.Context, n Notification) error {
	p := webhookPayload{Kind: n.Kind, Subject: n.Subject(), Time: n.At.Unix(), Items: make([]webhookItem, 0, len(n.Items))}
	for _, it := range n.Items {
		p.Items = append(p.Items, webhookItem{HostID: it.HostID, Hostname: it.Hostname, Service: it.Service, Type: it.Type, Message: it.Message, Since: it.Since.Unix()})
	}
	return postJSON(ctx, c.URL, c.Headers, p)
}

type discordConfig struct {
	WebhookURL string `json:"webhook_url"`
	Username   string `json:"username"`
}

// discordMaxContent is Discord's documented limit for message content.
const discordMaxContent = 2000

// Send posts to a Discord webhook. allowed_mentions is emptied so service
// names or program output containing @everyone cannot ping anyone.
func (c *discordConfig) Send(ctx context.Context, n Notification) error {
	payload := map[string]any{
		"content":          truncateRunes(n.Body(), discordMaxContent),
		"allowed_mentions": map[string]any{"parse": []string{}},
	}
	if c.Username != "" {
		payload["username"] = c.Username
	}
	return postJSON(ctx, c.WebhookURL, nil, payload)
}

var ntfyTopicRe = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

// ntfyMaxBody keeps below ntfy's 4096-byte limit, above which the message
// becomes an attachment instead of text.
const ntfyMaxBody = 4000

type ntfyConfig struct {
	Server   string `json:"server"`
	Topic    string `json:"topic"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Send publishes to <server>/<topic>. A token is sent as a Bearer token,
// otherwise username/password as Basic auth; both are optional.
func (c *ntfyConfig) Send(ctx context.Context, n Notification) error {
	priority, tags := "high", "warning"
	if n.Kind == KindRecovery || n.Kind == KindHostOnline {
		priority, tags = "default", "white_check_mark"
	}
	headers := map[string]string{
		"Title":    mime.BEncoding.Encode("UTF-8", n.Subject()),
		"Priority": priority,
		"Tags":     tags,
	}
	body := truncateBytes(n.Body(), ntfyMaxBody)
	target := strings.TrimRight(c.Server, "/") + "/" + c.Topic
	return doPost(ctx, target, headers, []byte(body), func(req *http.Request) {
		switch {
		case c.Token != "":
			req.Header.Set("Authorization", "Bearer "+c.Token)
		case c.Username != "":
			req.SetBasicAuth(c.Username, c.Password)
		}
	})
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}

// truncateBytes cuts at a rune boundary.
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - 3
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
