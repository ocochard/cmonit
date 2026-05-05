# cmonit Testing

## Test locations

| Path | Type | Runs against |
|------|------|--------------|
| `internal/parser/xml_test.go` | Unit — XML parsing | Synthetic XML fixtures, no network |
| `tests/api_test.go` | Integration — HTTP API | A live cmonit instance (requires `-url`) |

---

## Running tests

### Parser unit tests

```bash
go test ./internal/parser/
```

No flags needed. Uses hardcoded XML fixtures embedded in the test file.

### API integration tests

```bash
go test ./tests/ -url http://localhost:3000
```

The `-url` flag is required and must point to a running cmonit instance. No default is set — the test suite panics if the flag is omitted.

```bash
# Example against a remote instance
go test ./tests/ -url http://your-server:3000 -v

# With authentication
go test ./tests/ -url http://user:pass@your-server:3000 -v
```

### All tests

```bash
# Parser only (no live instance needed)
go test ./internal/...

# Everything including API (live instance required)
go test ./... -url http://localhost:3000
```

---

## API test coverage (`tests/api_test.go`)

| Test | Endpoint | What it checks |
|------|----------|----------------|
| TestHostGroupsAPI | GET /api/hostgroups | 200, `groups` key present |
| TestV2StatusHostsList | GET /api/2/status/hosts/list | 200, non-empty array, required fields |
| TestV2StatusHostsGetMissingID | GET /api/2/status/hosts/get | 400 when `id` omitted |
| TestV2StatusHostsGetNotFound | GET /api/2/status/hosts/get?id=… | 404 for unknown id |
| TestV2StatusHostsGetValid | GET /api/2/status/hosts/get?id=… | 200, id matches request |
| TestV2StatusHostsSummary | GET /api/2/status/hosts/summary | 200, `summary` key present |
| TestV2EventsList | GET /api/2/reports/events/list | 200, `records` and `events` keys |
| TestV2EventsGetMissingID | GET /api/2/reports/events/get | 400 when `id` omitted |
| TestV2EventsGetNotFound | GET /api/2/reports/events/get?id=999999 | 404 for unknown id |
| TestV2AdminHostsList | GET /api/2/admin/hosts/list | 200, `records` key present |
| TestV2AdminHostsDeleteMissingID | GET /api/2/admin/hosts/delete | 400 when `id` omitted |
| TestV2AdminHostsDeleteNotFound | GET /api/2/admin/hosts/delete?id=… | 404 for unknown id |

---

## Adding tests

Add new cases to `tests/api_test.go`. Use the `get()` helper for HTTP requests and `mustJSON` / `mustJSONArray` for response parsing. The test file has no build constraints — it compiles and runs as a standard Go test package.

Do not test destructive operations (delete of a real host) in the regression suite. The delete tests only verify error responses for non-existent hosts.
