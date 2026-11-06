package parser

import "testing"

func TestParseServiceErrorsEvents(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<monit id="x" incarnation="1" version="6.1.0">
<server><id>x</id><incarnation>1</incarnation><version>6.1.0</version><uptime>10</uptime><poll>30</poll><localhostname>web01</localhostname></server>
<platform><name>FreeBSD</name></platform>
<services>
<service name="app"><type>3</type><collected_sec>1</collected_sec><status>2</status><status_hint>0</status_hint><monitor>1</monitor>
<errors><event id="28" state="1"></event><event id="48" state="2"></event></errors></service>
<service name="ok"><type>3</type><collected_sec>1</collected_sec><status>0</status><monitor>1</monitor><errors></errors></service>
</services>
</monit>`
	st, err := ParseMonitXML([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Services) != 2 {
		t.Fatalf("services = %d", len(st.Services))
	}
	got := st.Services[0].Events
	if len(got) != 2 || got[0] != (ServiceEvent{ID: 28, State: 1}) || got[1] != (ServiceEvent{ID: 48, State: 2}) {
		t.Fatalf("events = %+v", got)
	}
	if len(st.Services[1].Events) != 0 {
		t.Fatalf("service without errors has events: %+v", st.Services[1].Events)
	}
}
