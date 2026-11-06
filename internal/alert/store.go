package alert

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Rule triggers.
const (
	TriggerServiceFailure = "service_failure"
	TriggerHostOffline    = "host_offline"
)

// MaskedSecret replaces secret config values in API responses. Posting it back
// on update keeps the stored value.
const MaskedSecret = "***"

// ErrNotFound is returned when a channel or rule id does not exist.
var ErrNotFound = errors.New("not found")

// InvalidError reports a caller mistake (bad field value) as opposed to an
// internal failure.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &InvalidError{Msg: fmt.Sprintf(format, args...)}
}

// IsInvalid reports whether err is an InvalidError.
func IsInvalid(err error) bool {
	var ie *InvalidError
	return errors.As(err, &ie)
}

// Channel is a notification medium instance (one SMTP account, one bot, ...).
type Channel struct {
	ID      int64           `json:"id"`
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Config  json.RawMessage `json:"config"`
	Enabled bool            `json:"enabled"`
}

// Rule selects which failures notify which channels.
type Rule struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	Enabled         bool    `json:"enabled"`
	Trigger         string  `json:"trigger"`
	HostPattern     string  `json:"host_pattern"`
	ServicePattern  string  `json:"service_pattern"`
	ServiceTypes    []int   `json:"service_types"`
	HoldSeconds     int     `json:"hold_seconds"`
	CooldownSeconds int     `json:"cooldown_seconds"`
	NotifyRecovery  bool    `json:"notify_recovery"`
	ChannelIDs      []int64 `json:"channel_ids"`

	Channels []Channel `json:"-"` // enabled channels, filled by LoadActiveRules
}

// LogEntry is one row of alert_log.
type LogEntry struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
	RuleID    *int64 `json:"rule_id"`
	ChannelID *int64 `json:"channel_id"`
	HostID    string `json:"host_id"`
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

var secretKeys = map[string]bool{"password": true, "bot_token": true, "webhook_url": true, "url": true}

// MaskConfig returns the config with secrets replaced by MaskedSecret.
func MaskConfig(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return json.RawMessage(`{}`)
	}
	for k, v := range m {
		if secretKeys[k] {
			if s, ok := v.(string); ok && s != "" {
				m[k] = MaskedSecret
			}
		}
		if k == "headers" {
			if h, ok := v.(map[string]any); ok {
				for hk := range h {
					h[hk] = MaskedSecret
				}
			}
		}
	}
	out, _ := json.Marshal(m)
	return out
}

// mergeSecrets restores masked values in next from the stored config.
func mergeSecrets(old, next json.RawMessage) json.RawMessage {
	var o, n map[string]any
	if json.Unmarshal(old, &o) != nil || json.Unmarshal(next, &n) != nil {
		return next
	}
	for k, v := range n {
		if s, ok := v.(string); ok && s == MaskedSecret {
			n[k] = o[k]
		}
		if k == "headers" {
			nh, _ := v.(map[string]any)
			oh, _ := o["headers"].(map[string]any)
			for hk, hv := range nh {
				if s, ok := hv.(string); ok && s == MaskedSecret {
					nh[hk] = oh[hk]
				}
			}
		}
	}
	out, _ := json.Marshal(n)
	return out
}

func ListChannels(db *sql.DB) ([]Channel, error) {
	rows, err := db.Query(`SELECT id, name, type, config, enabled FROM alert_channels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		var c Channel
		var cfg string
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &cfg, &c.Enabled); err != nil {
			return nil, err
		}
		c.Config = json.RawMessage(cfg)
		out = append(out, c)
	}
	return out, rows.Err()
}

func GetChannel(db *sql.DB, id int64) (Channel, error) {
	var c Channel
	var cfg string
	err := db.QueryRow(`SELECT id, name, type, config, enabled FROM alert_channels WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Type, &cfg, &c.Enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.Config = json.RawMessage(cfg)
	return c, err
}

// SaveChannel inserts (ID == 0) or updates a channel and sets c.ID.
func SaveChannel(db *sql.DB, c *Channel) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return invalid("name is required")
	}
	if len(c.Config) == 0 {
		c.Config = json.RawMessage(`{}`)
	}
	if c.ID != 0 {
		old, err := GetChannel(db, c.ID)
		if err != nil {
			return err
		}
		c.Config = mergeSecrets(old.Config, c.Config)
	}
	if _, err := NewNotifier(c.Type, c.Config); err != nil {
		return invalid("%v", err)
	}
	if c.ID == 0 {
		res, err := db.Exec(`INSERT INTO alert_channels (name, type, config, enabled) VALUES (?, ?, ?, ?)`,
			c.Name, c.Type, string(c.Config), c.Enabled)
		if err != nil {
			return err
		}
		c.ID, err = res.LastInsertId()
		return err
	}
	_, err := db.Exec(`UPDATE alert_channels SET name = ?, type = ?, config = ?, enabled = ? WHERE id = ?`,
		c.Name, c.Type, string(c.Config), c.Enabled, c.ID)
	return err
}

func DeleteChannel(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM alert_channels WHERE id = ?`, id)
	return checkAffected(res, err)
}

func checkAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func parseTypes(s string) []int {
	out := []int{}
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func joinTypes(t []int) string {
	parts := make([]string, len(t))
	for i, n := range t {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

const ruleColumns = `id, name, enabled, trigger, host_pattern, service_pattern, service_types, hold_seconds, cooldown_seconds, notify_recovery`

func scanRule(sc interface{ Scan(...any) error }) (Rule, error) {
	var r Rule
	var types string
	err := sc.Scan(&r.ID, &r.Name, &r.Enabled, &r.Trigger, &r.HostPattern, &r.ServicePattern, &types,
		&r.HoldSeconds, &r.CooldownSeconds, &r.NotifyRecovery)
	r.ServiceTypes = parseTypes(types)
	r.ChannelIDs = []int64{}
	return r, err
}

func attachChannelIDs(db *sql.DB, rules []Rule) error {
	byID := map[int64]*Rule{}
	for i := range rules {
		byID[rules[i].ID] = &rules[i]
	}
	rows, err := db.Query(`SELECT rule_id, channel_id FROM alert_rule_channels ORDER BY channel_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rid, cid int64
		if err := rows.Scan(&rid, &cid); err != nil {
			return err
		}
		if r := byID[rid]; r != nil {
			r.ChannelIDs = append(r.ChannelIDs, cid)
		}
	}
	return rows.Err()
}

func ListRules(db *sql.DB) ([]Rule, error) {
	rows, err := db.Query(`SELECT ` + ruleColumns + ` FROM alert_rules ORDER BY name`)
	if err != nil {
		return nil, err
	}
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, attachChannelIDs(db, out)
}

func GetRule(db *sql.DB, id int64) (Rule, error) {
	r, err := scanRule(db.QueryRow(`SELECT `+ruleColumns+` FROM alert_rules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	list := []Rule{r}
	if err := attachChannelIDs(db, list); err != nil {
		return r, err
	}
	return list[0], nil
}

// LoadActiveRules returns enabled rules with their enabled channels attached.
func LoadActiveRules(db *sql.DB) ([]Rule, error) {
	rules, err := ListRules(db)
	if err != nil {
		return nil, err
	}
	channels, err := ListChannels(db)
	if err != nil {
		return nil, err
	}
	enabled := map[int64]Channel{}
	for _, c := range channels {
		if c.Enabled {
			enabled[c.ID] = c
		}
	}
	var out []Rule
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		for _, id := range r.ChannelIDs {
			if c, ok := enabled[id]; ok {
				r.Channels = append(r.Channels, c)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// Validate normalizes defaults and rejects invalid values.
func (r *Rule) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return invalid("name is required")
	}
	if r.Trigger == "" {
		r.Trigger = TriggerServiceFailure
	}
	if r.Trigger != TriggerServiceFailure && r.Trigger != TriggerHostOffline {
		return invalid("trigger must be %q or %q", TriggerServiceFailure, TriggerHostOffline)
	}
	if r.HostPattern == "" {
		r.HostPattern = "*"
	}
	if r.ServicePattern == "" {
		r.ServicePattern = "*"
	}
	if r.HoldSeconds < 0 || r.CooldownSeconds < 0 {
		return invalid("hold_seconds and cooldown_seconds must be >= 0")
	}
	for _, t := range r.ServiceTypes {
		if t < 0 || t > 8 {
			return invalid("service type %d out of range 0-8", t)
		}
	}
	return nil
}

// SaveRule inserts (ID == 0) or updates a rule with its channel links and sets r.ID.
func SaveRule(db *sql.DB, r *Rule) error {
	if err := r.Validate(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if r.ID == 0 {
		res, err := tx.Exec(`INSERT INTO alert_rules (name, enabled, trigger, host_pattern, service_pattern, service_types, hold_seconds, cooldown_seconds, notify_recovery)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.Name, r.Enabled, r.Trigger, r.HostPattern, r.ServicePattern, joinTypes(r.ServiceTypes), r.HoldSeconds, r.CooldownSeconds, r.NotifyRecovery)
		if err != nil {
			return err
		}
		if r.ID, err = res.LastInsertId(); err != nil {
			return err
		}
	} else {
		res, err := tx.Exec(`UPDATE alert_rules SET name = ?, enabled = ?, trigger = ?, host_pattern = ?, service_pattern = ?, service_types = ?,
			hold_seconds = ?, cooldown_seconds = ?, notify_recovery = ? WHERE id = ?`,
			r.Name, r.Enabled, r.Trigger, r.HostPattern, r.ServicePattern, joinTypes(r.ServiceTypes), r.HoldSeconds, r.CooldownSeconds, r.NotifyRecovery, r.ID)
		if err := checkAffected(res, err); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM alert_rule_channels WHERE rule_id = ?`, r.ID); err != nil {
			return err
		}
	}
	for _, cid := range r.ChannelIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO alert_rule_channels (rule_id, channel_id) VALUES (?, ?)`, r.ID, cid); err != nil {
			return fmt.Errorf("channel %d: %w", cid, err)
		}
	}
	return tx.Commit()
}

func DeleteRule(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM alert_rules WHERE id = ?`, id)
	return checkAffected(res, err)
}

// ListLog returns the newest alert_log rows.
func ListLog(db *sql.DB, limit int) ([]LogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, created_at, rule_id, channel_id, host_id, kind, subject, status, error
		FROM alert_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.RuleID, &e.ChannelID, &e.HostID, &e.Kind, &e.Subject, &e.Status, &e.Error); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
