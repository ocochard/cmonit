package alert

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ocochard/cmonit/internal/parser"
)

const (
	flushInterval = 10 * time.Second
	sendTimeout   = 30 * time.Second
	defaultPoll   = 30
)

// Sender delivers a notification to a channel. Replaceable in tests.
type Sender func(ctx context.Context, ch Channel, n Notification) error

// Engine detects service/host state changes and sends grouped notifications.
//
// All state lives in alert_state, so a restart neither loses pending alerts
// nor re-notifies ongoing ones. Row semantics:
//
//	failed=1 notified=0  failure pending (waiting out the rule's hold time)
//	failed=1 notified=1  failure notified, still failing
//	failed=0 notified=1  recovered, recovery notification pending
//	failed=0 notified=0  idle (row kept for last_notified/cooldown)
//
// Cascade handling: failures are not sent immediately. Flush sends one
// notification per rule, across all hosts, once the oldest matching failure is
// older than the rule's hold time. Whatever fails inside that window (a
// database and the sites that depend on it, on any hosts) lands in one
// message, and services that recover during the hold are never reported.
// This groups by time only; it does not know which failure is the root cause
// (that needs explicit dependencies). A host that comes back from offline gets
// an extra grace period before its failing services are reported, and
// services of an offline host are not reported while it is offline.
type Engine struct {
	db   *sql.DB
	mu   sync.Mutex
	now  func() time.Time
	send Sender

	// One-shot "changed" events (timestamp, size, content match, ...). They
	// last one cycle, so they are sent as they are seen, without hold or
	// recovery, and only on the cycle they first appear. Kept in memory: a
	// restart loses at most events from the last few seconds.
	prevChanged map[string]map[string]map[int]bool // host -> service -> event ids
	oneShots    []oneShot
}

type oneShot struct {
	row stateRow
}

// maxPendingEvents bounds memory if flushing stalls.
const maxPendingEvents = 1000

// NewEngine returns an Engine that sends through the built-in notifiers.
func NewEngine(db *sql.DB) *Engine {
	return &Engine{db: db, now: time.Now, send: defaultSender, prevChanged: map[string]map[string]map[int]bool{}}
}

func defaultSender(ctx context.Context, ch Channel, n Notification) error {
	nt, err := NewNotifier(ch.Type, ch.Config)
	if err != nil {
		return err
	}
	return nt.Send(ctx, n)
}

// Run flushes pending notifications until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Flush(ctx)
		}
	}
}

type stateRow struct {
	HostID       string
	Hostname     string
	Service      string
	Type         int
	Failed       bool
	Notified     bool
	Since        int64
	LastNotified int64
	Message      string
}

func (r stateRow) key() string { return r.HostID + "\x00" + r.Service }

// Observe updates alert state from one collector POST. It must run after the
// status has been stored so the hosts row exists (alert_state references it).
func (e *Engine) Observe(hostID string, st *parser.MonitStatus) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.observe(hostID, st); err != nil {
		log.Printf("[WARN] Alert engine: observe %s: %v", hostID, err)
	}
}

func (e *Engine) observe(hostID string, st *parser.MonitStatus) error {
	now := e.now().Unix()

	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	cur := map[string]*stateRow{}
	rows, err := tx.Query(`SELECT service_name, failed, notified, since FROM alert_state WHERE host_id = ?`, hostID)
	if err != nil {
		return err
	}
	for rows.Next() {
		r := &stateRow{HostID: hostID}
		if err := rows.Scan(&r.Service, &r.Failed, &r.Notified, &r.Since); err != nil {
			rows.Close()
			return err
		}
		cur[r.Service] = r
	}
	if err := rows.Close(); err != nil {
		return err
	}

	// New failures after a host returns from offline are delayed by 2 poll
	// intervals: Monit re-evaluates every service on its first cycles and
	// reports transient failures that would otherwise page for nothing.
	newSince := now
	if host, ok := cur[""]; ok && host.Failed {
		poll := st.Server.Poll
		if poll <= 0 {
			poll = defaultPoll
		}
		newSince = now + int64(2*poll)
		if _, err := tx.Exec(`UPDATE alert_state SET failed = 0, since = ? WHERE host_id = ? AND service_name = ''`, now, hostID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE alert_state SET since = ? WHERE host_id = ? AND service_name != '' AND failed = 1 AND notified = 0`, newSince, hostID); err != nil {
			return err
		}
	}

	seen := make(map[string]bool, len(st.Services))
	changedNow := map[string]map[int]bool{}
	for i := range st.Services {
		svc := &st.Services[i]
		seen[svc.Name] = true
		e.recordChanged(hostID, st.Server.LocalHostname, svc, now, changedNow)

		if svc.Monitor == 2 { // initializing: state unknown, keep what we have
			continue
		}
		failed := isFailing(svc)
		prev := cur[svc.Name]

		switch {
		case failed && (prev == nil || !prev.Failed):
			// A failure that follows an unsent recovery (notified=1) continues
			// the already-notified incident instead of opening a new one.
			notified := prev != nil && prev.Notified
			// Monit's own check time, not our receive time: across agents it is
			// what lets the notification show which failure came first.
			since := now
			if svc.CollectedSec > 0 && svc.CollectedSec < now {
				since = svc.CollectedSec
			}
			if newSince > now {
				since = newSince
			}
			if notified {
				since = prev.Since
			}
			_, err = tx.Exec(`
				INSERT INTO alert_state (host_id, service_name, service_type, failed, since, notified, message)
				VALUES (?, ?, ?, 1, ?, ?, ?)
				ON CONFLICT(host_id, service_name) DO UPDATE SET
					service_type = excluded.service_type, failed = 1, since = excluded.since,
					notified = excluded.notified, message = excluded.message`,
				hostID, svc.Name, svc.Type, since, notified, describeFailure(svc))
		case failed:
			// still failing: refresh the message only
			_, err = tx.Exec(`UPDATE alert_state SET message = ? WHERE host_id = ? AND service_name = ?`,
				describeFailure(svc), hostID, svc.Name)
		case prev != nil && prev.Failed && svc.Monitor == 0:
			// unmonitored by the operator: close the incident without a recovery message
			_, err = tx.Exec(`UPDATE alert_state SET failed = 0, notified = 0 WHERE host_id = ? AND service_name = ?`,
				hostID, svc.Name)
		case prev != nil && prev.Failed:
			// recovered; notified stays as is, so a notified incident queues a recovery
			_, err = tx.Exec(`UPDATE alert_state SET failed = 0, since = ? WHERE host_id = ? AND service_name = ?`,
				now, hostID, svc.Name)
		}
		if err != nil {
			return err
		}
	}

	e.prevChanged[hostID] = changedNow

	// Services removed from the agent's configuration must not stay failed forever.
	for name := range cur {
		if name != "" && !seen[name] {
			if _, err := tx.Exec(`DELETE FROM alert_state WHERE host_id = ? AND service_name = ?`, hostID, name); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// recordChanged queues a one-shot for each changed event that was not already
// changed in the previous POST of the same host. Callers hold e.mu.
func (e *Engine) recordChanged(hostID, hostname string, svc *parser.Service, now int64, changedNow map[string]map[int]bool) {
	if svc.Monitor != 1 {
		return
	}
	ids := changedIDs(svc)
	if len(ids) == 0 {
		return
	}
	cur := map[int]bool{}
	prev := e.prevChanged[hostID][svc.Name]
	for _, id := range ids {
		cur[id] = true
		if prev[id] || len(e.oneShots) >= maxPendingEvents {
			continue
		}
		e.oneShots = append(e.oneShots, oneShot{row: stateRow{
			HostID: hostID, Hostname: hostname, Service: svc.Name, Type: svc.Type,
			Message: eventText(id, true), Since: now,
		}})
	}
	changedNow[svc.Name] = cur
}

// Flush marks silent hosts offline, then evaluates every enabled rule and
// sends the notifications that are due.
func (e *Engine) Flush(ctx context.Context) {
	if err := e.flush(ctx); err != nil {
		log.Printf("[WARN] Alert engine: flush: %v", err)
	}
}

func (e *Engine) flush(ctx context.Context) error {
	now := e.now()

	if err := e.markOfflineHosts(now); err != nil {
		return err
	}
	if _, err := e.db.Exec(`DELETE FROM alert_silences WHERE expires_at > 0 AND expires_at <= ?`, now.Unix()); err != nil {
		return err
	}
	rules, err := LoadActiveRules(e.db)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		e.takeOneShots() // nothing can match: do not let them pile up
		return nil
	}
	rows, err := e.loadStates()
	if err != nil {
		return err
	}

	silences, err := ListActiveSilences(e.db, now)
	if err != nil {
		return err
	}
	e.flushEvents(ctx, now, rules, silences)
	if len(silences) > 0 {
		rows = e.applySilences(silences, rows)
	}

	offline := map[string]bool{}
	for _, r := range rows {
		if r.Service == "" && r.Failed {
			offline[r.HostID] = true
		}
	}

	failureSent := map[string]stateRow{}
	recoverySent := map[string]stateRow{}
	recoveryWanted := map[string]bool{}

	for _, rule := range rules {
		e.evalRule(ctx, now, rule, rows, offline, failureSent, recoverySent, recoveryWanted)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range failureSent {
		e.db.Exec(`UPDATE alert_state SET notified = 1, last_notified = ? WHERE host_id = ? AND service_name = ? AND failed = 1 AND notified = 0`,
			now.Unix(), r.HostID, r.Service)
	}
	// Recoveries are cleared once sent, or immediately when no rule wants them.
	for _, r := range rows {
		if r.Failed || !r.Notified {
			continue
		}
		if _, sent := recoverySent[r.key()]; sent || !recoveryWanted[r.key()] {
			e.db.Exec(`UPDATE alert_state SET notified = 0 WHERE host_id = ? AND service_name = ? AND failed = 0 AND notified = 1`,
				r.HostID, r.Service)
		}
	}
	return nil
}

// applySilences removes silenced rows from the evaluation and settles their
// state without sending anything: a pending failure is marked as notified, so
// it stays quiet after the silence expires, and a pending recovery is cleared.
// It returns the rows that are not silenced.
func (e *Engine) applySilences(silences []Silence, rows []stateRow) []stateRow {
	e.mu.Lock()
	defer e.mu.Unlock()

	var kept []stateRow
	for _, r := range rows {
		if !isSilenced(silences, r) {
			kept = append(kept, r)
			continue
		}
		switch {
		case r.Failed && !r.Notified:
			e.db.Exec(`UPDATE alert_state SET notified = 1 WHERE host_id = ? AND service_name = ? AND failed = 1 AND notified = 0`, r.HostID, r.Service)
		case !r.Failed && r.Notified:
			e.db.Exec(`UPDATE alert_state SET notified = 0 WHERE host_id = ? AND service_name = ? AND failed = 0 AND notified = 1`, r.HostID, r.Service)
		}
	}
	return kept
}

func isSilenced(silences []Silence, r stateRow) bool {
	for _, s := range silences {
		if s.matches(r) {
			return true
		}
	}
	return false
}

func (e *Engine) takeOneShots() []oneShot {
	e.mu.Lock()
	defer e.mu.Unlock()
	pending := e.oneShots
	e.oneShots = nil
	return pending
}

// flushEvents sends the queued one-shot events to every service_failure rule
// that matches them, unless silenced. One notification per rule.
func (e *Engine) flushEvents(ctx context.Context, now time.Time, rules []Rule, silences []Silence) {
	pending := e.takeOneShots()
	if len(pending) == 0 {
		return
	}
	for _, rule := range rules {
		if rule.Trigger != TriggerServiceFailure {
			continue
		}
		var rows []stateRow
		for _, p := range pending {
			if !isSilenced(silences, p.row) && rule.matches(p.row) {
				rows = append(rows, p.row)
			}
		}
		if len(rows) > 0 {
			e.dispatch(ctx, now, rule, KindEvent, rows)
		}
	}
}

func (e *Engine) evalRule(ctx context.Context, now time.Time, rule Rule, rows []stateRow,
	offline map[string]bool, failureSent, recoverySent map[string]stateRow, recoveryWanted map[string]bool) {

	var fail, rec []stateRow
	for _, r := range rows {
		if !rule.matches(r) {
			continue
		}
		switch {
		case r.Failed && !r.Notified:
			// services of an offline host are held back; the host alert covers them
			if r.Service != "" && offline[r.HostID] {
				continue
			}
			if r.Since > now.Unix() {
				continue
			}
			if rule.CooldownSeconds > 0 && r.LastNotified > 0 && now.Unix()-r.LastNotified < int64(rule.CooldownSeconds) {
				continue
			}
			fail = append(fail, r)
		case !r.Failed && r.Notified && rule.NotifyRecovery:
			recoveryWanted[r.key()] = true
			rec = append(rec, r)
		}
	}

	if due(fail, now, rule.HoldSeconds) {
		kind := KindFailure
		if rule.Trigger == TriggerHostOffline {
			kind = KindHostOffline
		}
		if e.dispatch(ctx, now, rule, kind, fail) {
			for _, r := range fail {
				failureSent[r.key()] = r
			}
		}
	}
	if due(rec, now, rule.HoldSeconds) {
		kind := KindRecovery
		if rule.Trigger == TriggerHostOffline {
			kind = KindHostOnline
		}
		if e.dispatch(ctx, now, rule, kind, rec) {
			for _, r := range rec {
				recoverySent[r.key()] = r
			}
		}
	}
}

// due reports whether the oldest item has waited out the hold time.
func due(items []stateRow, now time.Time, holdSeconds int) bool {
	if len(items) == 0 {
		return false
	}
	oldest := items[0].Since
	for _, r := range items {
		if r.Since < oldest {
			oldest = r.Since
		}
	}
	return oldest+int64(holdSeconds) <= now.Unix()
}

// dispatch sends to every enabled channel of the rule and logs each attempt.
// It returns false only when there was nothing to send to, so the items are
// retried once a channel exists. A failed send is logged, not retried.
func (e *Engine) dispatch(ctx context.Context, now time.Time, rule Rule, kind string, rows []stateRow) bool {
	if len(rule.Channels) == 0 {
		return false
	}
	n := Notification{Kind: kind, At: now}
	for _, r := range rows {
		n.Items = append(n.Items, Item{HostID: r.HostID, Hostname: r.Hostname, Service: r.Service, Type: r.Type, Message: r.Message, Since: time.Unix(r.Since, 0)})
	}
	logHost := "*"
	if n.hostCount() == 1 {
		logHost = rows[0].HostID
	}
	for _, ch := range rule.Channels {
		sctx, cancel := context.WithTimeout(ctx, sendTimeout)
		err := e.send(sctx, ch, n)
		cancel()
		status, errMsg := "sent", ""
		if err != nil {
			status, errMsg = "failed", err.Error()
			log.Printf("[WARN] Alert engine: rule %q channel %q: %v", rule.Name, ch.Name, err)
		}
		if _, lerr := e.db.Exec(`INSERT INTO alert_log (rule_id, channel_id, host_id, kind, subject, status, error) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			rule.ID, ch.ID, logHost, kind, n.Subject(), status, errMsg); lerr != nil {
			log.Printf("[WARN] Alert engine: write alert_log: %v", lerr)
		}
	}
	return true
}

func (e *Engine) loadStates() ([]stateRow, error) {
	rows, err := e.db.Query(`
		SELECT s.host_id, h.hostname, s.service_name, s.service_type, s.failed, s.notified, s.since, s.last_notified, s.message
		FROM alert_state s JOIN hosts h ON h.id = s.host_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stateRow
	for rows.Next() {
		var r stateRow
		if err := rows.Scan(&r.HostID, &r.Hostname, &r.Service, &r.Type, &r.Failed, &r.Notified, &r.Since, &r.LastNotified, &r.Message); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// markOfflineHosts opens a host-level incident for hosts silent for 5 poll
// intervals (the same threshold as the red health state in the web UI).
// Recovery is detected in observe when data arrives again.
func (e *Engine) markOfflineHosts(now time.Time) error {
	rows, err := e.db.Query(`SELECT id, last_seen, poll_interval FROM hosts`)
	if err != nil {
		return err
	}
	type silent struct {
		id    string
		since time.Time
	}
	var hosts []silent
	for rows.Next() {
		var id string
		var lastSeen time.Time
		var poll sql.NullInt64
		if err := rows.Scan(&id, &lastSeen, &poll); err != nil {
			rows.Close()
			return err
		}
		interval := defaultPoll
		if poll.Valid && poll.Int64 > 0 {
			interval = int(poll.Int64)
		}
		if now.Sub(lastSeen) >= time.Duration(5*interval)*time.Second {
			hosts = append(hosts, silent{id, lastSeen})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, h := range hosts {
		msg := fmt.Sprintf("no data since %s", h.since.Format(time.RFC3339))
		// the WHERE keeps the original since of an already-failed row
		_, err := e.db.Exec(`
			INSERT INTO alert_state (host_id, service_name, failed, since, notified, message)
			VALUES (?, '', 1, ?, 0, ?)
			ON CONFLICT(host_id, service_name) DO UPDATE SET failed = 1, since = excluded.since, message = excluded.message
			WHERE alert_state.failed = 0`, h.id, now.Unix(), msg)
		if err != nil {
			return err
		}
	}
	return nil
}

// matches reports whether the rule applies to the state row.
func (r Rule) matches(s stateRow) bool {
	if !globMatch(r.HostPattern, s.Hostname) || r.excludes(s) {
		return false
	}
	if r.Trigger == TriggerHostOffline {
		return s.Service == ""
	}
	if s.Service == "" || !globMatch(r.ServicePattern, s.Service) {
		return false
	}
	if len(r.ServiceTypes) == 0 {
		return true
	}
	for _, t := range r.ServiceTypes {
		if t == s.Type {
			return true
		}
	}
	return false
}

// excludes reports whether the rule's exclusion covers the row. Both patterns
// must match; a host-level (offline) row is only excluded by service pattern "*".
func (r Rule) excludes(s stateRow) bool {
	if r.ExcludeServicePattern == "" || !globMatch(r.ExcludeHostPattern, s.Hostname) {
		return false
	}
	if s.Service == "" {
		return r.ExcludeServicePattern == "*"
	}
	return globMatch(r.ExcludeServicePattern, s.Service)
}

// globMatch supports * and ? and, unlike path.Match, lets * cross '/' because
// filesystem services are named after their mount point.
func globMatch(pattern, s string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	var b strings.Builder
	b.WriteString("^")
	for _, c := range pattern {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(s)
}

// SendTest sends a test notification through one channel.
func (e *Engine) SendTest(ctx context.Context, ch Channel) error {
	n := Notification{
		Kind: KindFailure, At: e.now(),
		Items: []Item{{HostID: "test", Hostname: "cmonit-test", Service: "test-service", Message: "test notification from cmonit", Since: e.now()}},
	}
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return e.send(sctx, ch, n)
}
