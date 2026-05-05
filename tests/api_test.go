// Package tests contains regression tests for the cmonit HTTP API.
//
// Usage:
//
//	go test ./tests/ -url http://localhost:3000
//	go test ./tests/ -url http://autobuilder.oca.netflix.net:3000
package tests

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"testing"
)

var baseURL = flag.String("url", "", "Base URL of the cmonit instance to test (required)")

func TestMain(m *testing.M) {
	flag.Parse()
	if *baseURL == "" {
		panic("flag -url is required, e.g. -url http://localhost:3000")
	}
	m.Run()
}

func get(t *testing.T, path string) ([]byte, int) {
	t.Helper()
	resp, err := http.Get(*baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body of %s: %v", path, err)
	}
	return body, resp.StatusCode
}

func mustJSON(t *testing.T, body []byte, path string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("GET %s: response is not valid JSON: %v\nbody: %s", path, err, body)
	}
	return out
}

func mustJSONArray(t *testing.T, body []byte, path string) []any {
	t.Helper()
	var out []any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("GET %s: response is not a JSON array: %v\nbody: %s", path, err, body)
	}
	return out
}

// --- Native API ---

func TestHostGroupsAPI(t *testing.T) {
	path := "/api/hostgroups"
	body, status := get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", path, status)
	}
	obj := mustJSON(t, body, path)
	if _, ok := obj["groups"]; !ok {
		t.Errorf("GET %s: response missing 'groups' key", path)
	}
}

// --- M/Monit v2 API: Status ---

func TestV2StatusHostsList(t *testing.T) {
	path := "/api/2/status/hosts/list"
	body, status := get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", path, status)
	}
	hosts := mustJSONArray(t, body, path)
	if len(hosts) == 0 {
		t.Errorf("GET %s: expected at least one host, got empty list", path)
	}
	// Verify required fields on first host
	first, ok := hosts[0].(map[string]any)
	if !ok {
		t.Fatalf("GET %s: first element is not an object", path)
	}
	for _, field := range []string{"id", "hostname", "status", "lastseen"} {
		if _, ok := first[field]; !ok {
			t.Errorf("GET %s: host missing field %q", path, field)
		}
	}
}

func TestV2StatusHostsGetMissingID(t *testing.T) {
	path := "/api/2/status/hosts/get"
	_, status := get(t, path)
	if status != http.StatusBadRequest {
		t.Errorf("GET %s (no id): expected 400, got %d", path, status)
	}
}

func TestV2StatusHostsGetNotFound(t *testing.T) {
	path := "/api/2/status/hosts/get?id=nonexistent-host-0"
	_, status := get(t, path)
	if status != http.StatusNotFound {
		t.Errorf("GET %s: expected 404, got %d", path, status)
	}
}

func TestV2StatusHostsGetValid(t *testing.T) {
	// First get a real host id from the list
	listBody, _ := get(t, "/api/2/status/hosts/list")
	hosts := mustJSONArray(t, listBody, "/api/2/status/hosts/list")
	if len(hosts) == 0 {
		t.Skip("no hosts available to test /api/2/status/hosts/get")
	}
	first := hosts[0].(map[string]any)
	id := first["id"].(string)

	path := fmt.Sprintf("/api/2/status/hosts/get?id=%s", id)
	body, status := get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", path, status)
	}
	obj := mustJSON(t, body, path)
	if obj["id"] != id {
		t.Errorf("GET %s: expected id %q, got %v", path, id, obj["id"])
	}
}

func TestV2StatusHostsSummary(t *testing.T) {
	path := "/api/2/status/hosts/summary"
	body, status := get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", path, status)
	}
	obj := mustJSON(t, body, path)
	if _, ok := obj["summary"]; !ok {
		t.Errorf("GET %s: response missing 'summary' key", path)
	}
}

// --- M/Monit v2 API: Events ---

func TestV2EventsList(t *testing.T) {
	path := "/api/2/reports/events/list"
	body, status := get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", path, status)
	}
	obj := mustJSON(t, body, path)
	if _, ok := obj["records"]; !ok {
		t.Errorf("GET %s: response missing 'records' key", path)
	}
	if _, ok := obj["events"]; !ok {
		t.Errorf("GET %s: response missing 'events' key", path)
	}
}

func TestV2EventsGetMissingID(t *testing.T) {
	path := "/api/2/reports/events/get"
	_, status := get(t, path)
	if status != http.StatusBadRequest {
		t.Errorf("GET %s (no id): expected 400, got %d", path, status)
	}
}

func TestV2EventsGetNotFound(t *testing.T) {
	path := "/api/2/reports/events/get?id=999999"
	_, status := get(t, path)
	if status != http.StatusNotFound {
		t.Errorf("GET %s: expected 404, got %d", path, status)
	}
}

// --- M/Monit v2 API: Admin ---

func TestV2AdminHostsList(t *testing.T) {
	path := "/api/2/admin/hosts/list"
	body, status := get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", path, status)
	}
	obj := mustJSON(t, body, path)
	if _, ok := obj["records"]; !ok {
		t.Errorf("GET %s: response missing 'records' key", path)
	}
}

func TestV2AdminHostsDeleteMissingID(t *testing.T) {
	path := "/api/2/admin/hosts/delete"
	_, status := get(t, path)
	if status != http.StatusBadRequest {
		t.Errorf("GET %s (no id): expected 400, got %d", path, status)
	}
}

func TestV2AdminHostsDeleteNotFound(t *testing.T) {
	path := "/api/2/admin/hosts/delete?id=nonexistent-host-0"
	_, status := get(t, path)
	if status != http.StatusNotFound {
		t.Errorf("GET %s: expected 404, got %d", path, status)
	}
}
