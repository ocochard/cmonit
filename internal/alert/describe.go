package alert

import (
	"fmt"
	"strings"

	"github.com/ocochard/cmonit/internal/parser"
)

// Bit values of Monit's legacy <status> field (EventClass_T in monit src/event.h).
var statusBits = []struct {
	bit  int
	text string
}{
	{0x1, "checksum failed"},
	{0x2, "resource limit matched"},
	{0x4, "timeout"},
	{0x8, "timestamp failed"},
	{0x10, "size failed"},
	{0x20, "connection failed"},
	{0x40, "permission failed"},
	{0x80, "uid failed"},
	{0x100, "gid failed"},
	{0x200, "does not exist"},
	{0x400, "invalid type"},
	{0x800, "data access error"},
	{0x1000, "execution failed"},
	{0x2000, "filesystem flags failed"},
	{0x4000, "ICMP failed"},
	{0x8000, "content match"},
	{0x10000, "instance check failed"},
	{0x20000, "action failed"},
	{0x40000, "pid changed"},
	{0x80000, "ppid changed"},
	{0x100000, "heartbeat failed"},
	{0x200000, "status failed"},
	{0x400000, "uptime failed"},
	{0x800000, "link down"},
	{0x1000000, "speed changed"},
	{0x2000000, "saturation exceeded"},
	{0x4000000, "download bytes exceeded"},
	{0x8000000, "upload bytes exceeded"},
	{0x10000000, "download packets exceeded"},
	{0x20000000, "upload packets exceeded"},
	{0x40000000, "exists"},
}

// legacyClassNames name the legacy status bits for "changed" messages from
// agents that do not send <errors> (Monit 6.0).
var legacyClassNames = map[int]string{
	0x1:        "checksum",
	0x2:        "resource",
	0x4:        "timeout",
	0x8:        "timestamp",
	0x10:       "size",
	0x20:       "connection",
	0x40:       "permission",
	0x80:       "uid",
	0x100:      "gid",
	0x200:      "existence",
	0x400:      "invalid type",
	0x800:      "data",
	0x1000:     "exec",
	0x2000:     "filesystem flags",
	0x4000:     "icmp",
	0x8000:     "content",
	0x10000:    "instance",
	0x20000:    "action",
	0x40000:    "pid",
	0x80000:    "ppid",
	0x100000:   "heartbeat",
	0x200000:   "status",
	0x400000:   "uptime",
	0x800000:   "link",
	0x1000000:  "speed",
	0x2000000:  "saturation",
	0x4000000:  "download bytes",
	0x8000000:  "upload bytes",
	0x10000000: "download packets",
	0x20000000: "upload packets",
	0x40000000: "existence",
}

const maxOutputLen = 200

// Per-event states in <errors><event state="..."> (State_Type in monit.h).
const (
	stateFailed  = 1
	stateChanged = 2
)

func eventsInState(svc *parser.Service, state int) []int {
	var ids []int
	for _, ev := range svc.Events {
		if ev.State == state {
			ids = append(ids, ev.ID)
		}
	}
	return ids
}

// isFailing reports whether a monitored service has a failed test. Monit's
// legacy status bitmask also carries "changed" events (timestamp, size,
// content match, link speed), which last one cycle and are not failures;
// status_hint marks those bits. Agents that send <errors> give the exact
// per-test states, otherwise the bitmask minus the hint bits is used.
func isFailing(svc *parser.Service) bool {
	if svc.Monitor != 1 {
		return false
	}
	if len(svc.Events) > 0 {
		return len(eventsInState(svc, stateFailed)) > 0
	}
	return svc.Status&^svc.StatusHint != 0
}

// changedIDs lists the changed events of a service. Agents with <errors> give
// exact event ids; older agents only mark whole classes in status_hint, which
// are returned as negative ids (-bit) and described by eventText.
func changedIDs(svc *parser.Service) []int {
	if len(svc.Events) > 0 {
		return eventsInState(svc, stateChanged)
	}
	var ids []int
	for bit := 1; bit <= 0x40000000; bit <<= 1 {
		if svc.StatusHint&svc.Status&bit != 0 {
			ids = append(ids, -bit)
		}
	}
	return ids
}

func eventText(id int, changed bool) string {
	if id < 0 {
		name := legacyClassNames[-id]
		if name == "" {
			name = fmt.Sprintf("status bit 0x%x", -id)
		}
		if name == "content" {
			return "content match"
		}
		return name + " changed"
	}
	if id >= len(monitEvents) {
		return fmt.Sprintf("event %d", id)
	}
	if changed {
		return monitEvents[id].Changed
	}
	return monitEvents[id].Failed
}

// describeFailure turns a failing service into the text shown in notifications.
// Program checks add the exit status and the first line of output, which is
// usually what the operator needs to see.
func describeFailure(svc *parser.Service) string {
	var parts []string
	for _, id := range eventsInState(svc, stateFailed) {
		parts = append(parts, eventText(id, false))
	}
	if len(parts) == 0 {
		for _, b := range statusBits {
			if svc.Status&^svc.StatusHint&b.bit != 0 {
				parts = append(parts, b.text)
			}
		}
	}
	msg := strings.Join(parts, ", ")
	if msg == "" {
		msg = fmt.Sprintf("failed (status %d)", svc.Status)
	}
	if svc.Program != nil && svc.Program.Status != 0 {
		msg += fmt.Sprintf("; exit status %d", svc.Program.Status)
		if out := firstLine(svc.Program.Output); out != "" {
			msg += ": " + out
		}
	}
	return msg
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > maxOutputLen {
		s = s[:maxOutputLen] + "..."
	}
	return s
}
