package alert

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ocochard/cmonit/internal/db"
	"github.com/ocochard/cmonit/internal/parser"
)

type harness struct {
	t     *testing.T
	db    *sql.DB
	eng   *Engine
	clock time.Time
	mu    sync.Mutex
	sent  []Notification
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	d, err := db.InitDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	h := &harness{t: t, db: d, clock: time.Now()}
	h.eng = NewEngine(d)
	h.eng.now = func() time.Time { return h.clock }
	h.eng.send = func(_ context.Context, _ Channel, n Notification) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.sent = append(h.sent, n)
		return nil
	}

	if _, err := d.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES ('h1', 'web01', ?, 30)`, h.clock); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) addRule(r Rule) {
	h.t.Helper()
	ch := Channel{Name: "c" + r.Name, Type: "webhook", Config: []byte(`{"url":"http://x"}`), Enabled: true}
	if err := SaveChannel(h.db, &ch); err != nil {
		h.t.Fatal(err)
	}
	r.Enabled = true
	r.ChannelIDs = []int64{ch.ID}
	if err := SaveRule(h.db, &r); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) advance(d time.Duration) { h.clock = h.clock.Add(d) }

func (h *harness) post(failing ...string) {
	h.t.Helper()
	down := map[string]bool{}
	for _, f := range failing {
		down[f] = true
	}
	st := &parser.MonitStatus{Server: parser.Server{Poll: 30}}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("svc%d", i)
		s := parser.Service{Name: name, Type: 3, Monitor: 1}
		if down[name] {
			s.Status = 1
		}
		st.Services = append(st.Services, s)
	}
	h.eng.Observe("h1", st)
	h.touch()
}

// touch simulates the host row update that StoreHost does on every POST.
func (h *harness) touch() {
	h.db.Exec(`UPDATE hosts SET last_seen = ? WHERE id = 'h1'`, h.clock)
}

func (h *harness) flush() { h.eng.Flush(context.Background()) }

func (h *harness) kinds() []string {
	var k []string
	for _, n := range h.sent {
		k = append(k, fmt.Sprintf("%s:%d", n.Kind, len(n.Items)))
	}
	return k
}

func (h *harness) expect(want ...string) {
	h.t.Helper()
	got := h.kinds()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		h.t.Fatalf("notifications = %v, want %v", got, want)
	}
}

func TestCascadeCollapsesToOneNotification(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 30, NotifyRecovery: true})

	h.post("svc0", "svc1", "svc2", "svc3", "svc4")
	h.flush()
	h.expect() // still inside the hold window

	h.advance(31 * time.Second)
	h.post("svc0", "svc1", "svc2", "svc3", "svc4")
	h.flush()
	h.expect("failure:5")

	h.advance(time.Minute)
	h.post("svc0", "svc1", "svc2", "svc3", "svc4")
	h.flush()
	h.expect("failure:5") // ongoing failure is not re-sent
}

func TestFlapInsideHoldIsAbsorbed(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 30, NotifyRecovery: true})

	h.post("svc0")
	h.advance(10 * time.Second)
	h.post() // recovered before the hold elapsed
	h.advance(60 * time.Second)
	h.post()
	h.flush()
	h.expect()
}

func TestRecoveryIsSentOnceAfterFailure(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0, NotifyRecovery: true})

	h.post("svc0", "svc1")
	h.flush()
	h.advance(30 * time.Second)
	h.post("svc0") // svc1 recovers
	h.flush()
	h.flush()
	h.expect("failure:2", "recovery:1")
}

func TestNoRecoveryWhenDisabled(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0, NotifyRecovery: false})

	h.post("svc0")
	h.flush()
	h.advance(30 * time.Second)
	h.post()
	h.flush()
	h.expect("failure:1")
}

func TestRuleFilters(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "only-svc1", HoldSeconds: 0, ServicePattern: "svc1"})

	h.post("svc0", "svc1")
	h.flush()
	h.expect("failure:1")
	if h.sent[0].Items[0].Service != "svc1" {
		t.Fatalf("matched %q, want svc1", h.sent[0].Items[0].Service)
	}
}

func TestCooldownDefersFlappingService(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0, CooldownSeconds: 300})

	h.post("svc0")
	h.flush()
	h.advance(30 * time.Second)
	h.post()
	h.flush()
	h.advance(30 * time.Second)
	h.post("svc0")
	h.flush()
	h.expect("failure:1") // second failure is inside the cooldown

	h.advance(301 * time.Second)
	h.post("svc0")
	h.flush()
	h.expect("failure:1", "failure:1") // still failing after the cooldown
}

func TestHostOfflineSuppressesServicesAndRecovers(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "svc", HoldSeconds: 0, NotifyRecovery: true})
	h.addRule(Rule{Name: "host", Trigger: TriggerHostOffline, HoldSeconds: 0, NotifyRecovery: true})

	h.post("svc0") // pending, flushed below only after the host is silent
	h.advance(10 * time.Minute)
	h.flush()
	h.expect("host_offline:1") // service failure is held back

	// host returns with svc0 still failing: reported only after the grace period
	h.post("svc0")
	h.flush()
	h.expect("host_offline:1", "host_online:1")

	h.advance(2 * time.Minute)
	h.post("svc0")
	h.flush()
	h.expect("host_offline:1", "host_online:1", "failure:1")
}

func TestCascadeAcrossHostsIsOneNotification(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 30})
	if _, err := h.db.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES ('h2', 'db01', ?, 30)`, h.clock); err != nil {
		t.Fatal(err)
	}

	h.post("svc0") // web01: site down
	h.eng.Observe("h2", &parser.MonitStatus{Server: parser.Server{Poll: 30}, Services: []parser.Service{{Name: "postgres", Type: 3, Monitor: 1, Status: 1}}})
	h.advance(31 * time.Second)
	h.flush()
	h.expect("failure:2")
	if got := h.sent[0].hostCount(); got != 2 {
		t.Fatalf("hosts in notification = %d, want 2", got)
	}
}

func TestNotificationOrdersByMonitCheckTime(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 30})
	if _, err := h.db.Exec(`INSERT INTO hosts (id, hostname, last_seen, poll_interval) VALUES ('h2', 'db01', ?, 30)`, h.clock); err != nil {
		t.Fatal(err)
	}
	base := h.clock.Unix()

	// web01 is observed first but its check ran 10s after db01's
	h.eng.Observe("h1", &parser.MonitStatus{Server: parser.Server{Poll: 30}, Services: []parser.Service{
		{Name: "site", Type: 3, Monitor: 1, Status: 1, CollectedSec: base - 5}}})
	h.eng.Observe("h2", &parser.MonitStatus{Server: parser.Server{Poll: 30}, Services: []parser.Service{
		{Name: "postgres", Type: 3, Monitor: 1, Status: 1, CollectedSec: base - 15}}})
	h.advance(31 * time.Second)
	h.flush()

	h.expect("failure:2")
	n := h.sent[0]
	if got := n.chronological()[0].Service; got != "postgres" {
		t.Fatalf("first item = %q, want postgres", got)
	}
	want := "[cmonit] 2 services failing on 2 host(s) (first: postgres@db01)"
	if n.Subject() != want {
		t.Fatalf("subject = %q, want %q", n.Subject(), want)
	}
}

func TestRemovedServiceClearsState(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0})

	h.post("svc0")
	h.eng.Observe("h1", &parser.MonitStatus{Server: parser.Server{Poll: 30}}) // agent no longer reports svc0
	var n int
	h.db.QueryRow(`SELECT COUNT(*) FROM alert_state WHERE service_name = 'svc0'`).Scan(&n)
	if n != 0 {
		t.Fatalf("alert_state rows for removed service = %d, want 0", n)
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "anything", true},
		{"web*", "web01", true},
		{"web*", "db01", false},
		{"/var/*", "/var/log", true},
		{"a?c", "abc", true},
		{"a.c", "abc", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestChannelSecretsMaskedAndPreserved(t *testing.T) {
	h := newHarness(t)
	ch := Channel{Name: "tg", Type: "telegram", Config: []byte(`{"bot_token":"SECRET","chat_id":"1"}`), Enabled: true}
	if err := SaveChannel(h.db, &ch); err != nil {
		t.Fatal(err)
	}
	if got := string(MaskConfig(ch.Config)); got != `{"bot_token":"***","chat_id":"1"}` {
		t.Fatalf("masked = %s", got)
	}
	upd := Channel{ID: ch.ID, Name: "tg", Type: "telegram", Config: []byte(`{"bot_token":"***","chat_id":"2"}`), Enabled: true}
	if err := SaveChannel(h.db, &upd); err != nil {
		t.Fatal(err)
	}
	stored, _ := GetChannel(h.db, ch.ID)
	if string(stored.Config) != `{"bot_token":"SECRET","chat_id":"2"}` {
		t.Fatalf("stored = %s", stored.Config)
	}
}

func TestDescribeFailure(t *testing.T) {
	cases := []struct {
		name string
		svc  parser.Service
		want string
	}{
		{"process missing", parser.Service{Status: 0x200}, "does not exist"},
		{"program", parser.Service{Status: 0x200000, Program: &parser.ProgramInfo{Status: 1, Output: "sensor timeout\nsecond line"}},
			"status failed; exit status 1: sensor timeout"},
		{"several bits", parser.Service{Status: 0x2 | 0x20}, "resource limit matched, connection failed"},
		{"unknown bit only", parser.Service{Status: 0x80000000}, "failed (status 2147483648)"},
	}
	for _, c := range cases {
		if got := describeFailure(&c.svc); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func (h *harness) silence(host, service string, d time.Duration) {
	h.t.Helper()
	s := Silence{HostPattern: host, ServicePattern: service, DurationSeconds: int64(d.Seconds())}
	if err := SaveSilence(h.db, &s, h.clock); err != nil {
		h.t.Fatal(err)
	}
}

func TestSilenceSuppressesFailureAndStaysQuietAfterExpiry(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0, NotifyRecovery: true})
	h.silence("web01", "*", time.Hour)

	h.post("svc0")
	h.flush()
	h.expect() // silenced

	h.advance(2 * time.Hour) // silence expired, svc0 still failing
	h.post("svc0")
	h.flush()
	h.expect() // known failure stays quiet

	h.post("svc0", "svc1") // a new failure after the silence alerts normally
	h.flush()
	h.expect("failure:1")
	if got := h.sent[0].Items[0].Service; got != "svc1" {
		t.Fatalf("alerted on %q, want svc1", got)
	}
}

func TestSilenceSuppressesRecoveryOfNotifiedFailure(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0, NotifyRecovery: true})

	h.post("svc0")
	h.flush()
	h.expect("failure:1")

	h.silence("*", "*", time.Hour)
	h.advance(30 * time.Second)
	h.post() // recovers while silenced
	h.flush()
	h.expect("failure:1") // no recovery message

	h.advance(2 * time.Hour)
	h.post()
	h.flush()
	h.expect("failure:1") // and none after expiry
}

func TestSilenceMatchesByHostAndService(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0})
	h.silence("other*", "*", time.Hour)   // different host
	h.silence("web01", "svc1", time.Hour) // only one service

	h.post("svc0", "svc1")
	h.flush()
	h.expect("failure:1")
	if got := h.sent[0].Items[0].Service; got != "svc0" {
		t.Fatalf("alerted on %q, want svc0", got)
	}
}

func TestSilenceUntilRemovedAndDelete(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0})
	h.silence("web01", "*", 0) // until removed

	h.advance(1000 * time.Hour)
	list, err := ListActiveSilences(h.db, h.clock)
	if err != nil || len(list) != 1 || list[0].ExpiresAt != 0 {
		t.Fatalf("silences = %+v, err = %v", list, err)
	}
	if err := DeleteSilence(h.db, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSilence(h.db, list[0].ID); err != ErrNotFound {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestSilenceOnlyHostWideCoversOfflineIncident(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "host", Trigger: TriggerHostOffline, HoldSeconds: 0})
	h.silence("web01", "svc1", time.Hour) // service-specific: must not hide the host incident

	h.advance(10 * time.Minute)
	h.flush()
	h.expect("host_offline:1")
}

func TestSilenceRejectsNegativeDuration(t *testing.T) {
	h := newHarness(t)
	s := Silence{DurationSeconds: -1}
	if err := SaveSilence(h.db, &s, h.clock); !IsInvalid(err) {
		t.Fatalf("err = %v, want InvalidError", err)
	}
}

func (h *harness) postSvc(svcs ...parser.Service) {
	h.t.Helper()
	h.eng.Observe("h1", &parser.MonitStatus{Server: parser.Server{Poll: 30, LocalHostname: "web01"}, Services: svcs})
	h.touch()
}

func changedSvc(name string, ids ...int) parser.Service {
	s := parser.Service{Name: name, Type: 2, Monitor: 1, Status: 0x8, StatusHint: 0x8}
	for _, id := range ids {
		s.Events = append(s.Events, parser.ServiceEvent{ID: id, State: stateChanged})
	}
	return s
}

func TestChangedOnlyServiceIsNotFailing(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0})

	h.postSvc(changedSvc("conf", 48)) // timestamp changed
	h.advance(time.Minute)
	h.postSvc(changedSvc("conf", 48))
	h.flush()

	var failed int
	h.db.QueryRow(`SELECT COUNT(*) FROM alert_state WHERE failed = 1`).Scan(&failed)
	if failed != 0 {
		t.Fatalf("failed state rows = %d, want 0", failed)
	}
	for _, n := range h.sent {
		if n.Kind != KindEvent {
			t.Fatalf("got %s notification for a changed-only service", n.Kind)
		}
	}
}

func TestChangedEventNotifiesOnceOnFirstCycle(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 30})

	h.postSvc(changedSvc("conf", 48))
	h.flush()
	h.expect("event:1") // no hold for one-shot events
	if got := h.sent[0].Items[0].Message; got != monitEvents[48].Changed {
		t.Fatalf("message = %q, want %q", got, monitEvents[48].Changed)
	}

	h.advance(30 * time.Second)
	h.postSvc(changedSvc("conf", 48)) // still changed the next cycle: same occurrence
	h.flush()
	h.expect("event:1")

	h.advance(30 * time.Second)
	h.postSvc(parser.Service{Name: "conf", Type: 2, Monitor: 1}) // cleared
	h.advance(30 * time.Second)
	h.postSvc(changedSvc("conf", 48)) // new occurrence
	h.flush()
	h.expect("event:1", "event:1")
}

func TestChangedEventHonorsSilenceAndRuleFilter(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "other", HoldSeconds: 0, ServicePattern: "other"})
	h.postSvc(changedSvc("conf", 48))
	h.flush()
	h.expect() // rule filter

	h2 := newHarness(t)
	h2.addRule(Rule{Name: "all", HoldSeconds: 0})
	h2.silence("web01", "*", time.Hour)
	h2.postSvc(changedSvc("conf", 48))
	h2.flush()
	h2.expect() // silenced
}

func TestEventsDropWhenNoRules(t *testing.T) {
	h := newHarness(t)
	h.postSvc(changedSvc("conf", 48))
	h.flush()
	if n := len(h.eng.oneShots); n != 0 {
		t.Fatalf("pending one-shots = %d, want 0", n)
	}
}

func TestFailureMessageNamesTheFailedTest(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 0})

	// legacy status says "resource" for memory, cpu, load...; <errors> says which
	h.postSvc(parser.Service{Name: "app", Type: 3, Monitor: 1, Status: 0x2,
		Events: []parser.ServiceEvent{{ID: 28, State: stateFailed}}})
	h.flush()
	h.expect("failure:1")
	if got := h.sent[0].Items[0].Message; got != monitEvents[28].Failed {
		t.Fatalf("message = %q, want %q", got, monitEvents[28].Failed)
	}
}

func TestFailureMixedWithChangedUsesOnlyFailed(t *testing.T) {
	svc := parser.Service{Monitor: 1, Status: 0x8 | 0x200, StatusHint: 0x8,
		Events: []parser.ServiceEvent{{ID: 48, State: stateChanged}, {ID: 14, State: stateFailed}}}
	if !isFailing(&svc) {
		t.Fatal("service with a failed event must be failing")
	}
	if got := describeFailure(&svc); got != monitEvents[14].Failed {
		t.Fatalf("message = %q, want %q", got, monitEvents[14].Failed)
	}
}

func TestLegacyAgentWithoutErrorsList(t *testing.T) {
	// no <errors>: fall back to the bitmask, minus the bits status_hint marks as changed
	svc := parser.Service{Monitor: 1, Status: 0x8 | 0x200, StatusHint: 0x8}
	if !isFailing(&svc) {
		t.Fatal("non-hint bit must count as failing")
	}
	if got := describeFailure(&svc); got != "does not exist" {
		t.Fatalf("message = %q", got)
	}
	onlyChanged := parser.Service{Monitor: 1, Status: 0x8, StatusHint: 0x8}
	if isFailing(&onlyChanged) {
		t.Fatal("hint-only status must not count as failing")
	}
	unmonitored := parser.Service{Monitor: 0, Status: 0x200}
	if isFailing(&unmonitored) {
		t.Fatal("unmonitored service must not count as failing")
	}
}

func TestLegacyAgentChangedEventFromHintBits(t *testing.T) {
	h := newHarness(t)
	h.addRule(Rule{Name: "all", HoldSeconds: 30})

	// Monit 6.0: no <errors>, status_hint marks the "changed" class bits
	legacy := parser.Service{Name: "conf", Type: 2, Monitor: 1, Status: 0x8 | 0x8000, StatusHint: 0x8 | 0x8000}
	h.postSvc(legacy)
	h.flush()
	h.expect("event:2")
	msgs := []string{h.sent[0].Items[0].Message, h.sent[0].Items[1].Message}
	if !(contains(msgs, "timestamp changed") && contains(msgs, "content match")) {
		t.Fatalf("messages = %v", msgs)
	}
	if isFailing(&legacy) {
		t.Fatal("hint-only status must not be a failure")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
