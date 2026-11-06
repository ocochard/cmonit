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

const maxOutputLen = 200

// describeFailure turns a failing service into the text shown in notifications.
// Program checks add the exit status and the first line of output, which is
// usually what the operator needs to see.
func describeFailure(svc *parser.Service) string {
	var parts []string
	for _, b := range statusBits {
		if svc.Status&b.bit != 0 {
			parts = append(parts, b.text)
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
