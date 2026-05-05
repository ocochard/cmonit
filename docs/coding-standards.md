# cmonit Coding Standards

## Comment Philosophy

Write comments only when the **why** is non-obvious: a hidden constraint, a subtle invariant, a workaround for a known bug, or behaviour that would surprise a reader. Do not comment what the code does — well-named identifiers already do that.

### What not to comment

```go
// Bad: restates the code
// Increment the counter
count++

// Bad: describes parameters that are self-evident
// name is the hostname string
func storeHost(name string) error {
```

### What to comment

```go
// Good: explains a non-obvious constraint
// SQLite's COALESCE preserves created_at across upserts; without it every
// update would reset the creation timestamp.
const query = `INSERT OR REPLACE INTO hosts (...) VALUES (?, COALESCE(
    (SELECT created_at FROM hosts WHERE id = ?), ?))`

// Good: explains a workaround
// modernc/sqlite does not expose sqlite3_interrupt, so we rely on context
// cancellation instead.
```

## Function comments

Use Go doc format for exported functions. One sentence stating what it does; add a note only if the behaviour is surprising.

```go
// StoreHost inserts or updates a host record. It resets monit_uptime to zero
// and emits a restart event if the new uptime is lower than expected from the
// elapsed wall-clock time.
func StoreHost(db *sql.DB, host *parser.Server) error {
```

Private functions need a comment only when the name alone is insufficient to understand the purpose.

## Struct and field comments

Comment a field only if its meaning or unit is not obvious from the name.

```go
type MMHostSummary struct {
    ID         string `json:"id"`
    Hostname   string `json:"hostname"`
    MonitUptime int64 `json:"monituptime"` // seconds, not wall-clock uptime
}
```

## Error handling

Return errors; do not swallow them silently. Log at the call site that has context, not deep in helpers.

```go
// Good
if err != nil {
    log.Printf("[ERROR] StoreHost %s: %v", hostname, err)
    return err
}

// Bad: silent discard
db.Exec(query, args...)
```

## SQL queries

Name the constant, keep it readable with indentation. No inline comment needed unless the SQL itself is tricky.

```go
const query = `
    SELECT id, hostname, last_seen
    FROM hosts
    WHERE poll_interval > 0
    ORDER BY hostname
`
```

## Imports

Group: stdlib, then third-party, then internal. Use the blank import only for driver registration; put it in a separate group with a short note.

```go
import (
    "database/sql"
    "net/http"

    _ "modernc.org/sqlite" // registers "sqlite" driver

    dbpkg "github.com/ocochard/cmonit/internal/db"
)
```

## General rules

- No multi-paragraph docstrings or tutorial-style explanations in source files.
- No comments that reference the task, fix, or PR that introduced the code.
- Three similar lines is better than a premature abstraction.
- Prefer editing existing files to creating new ones.
- Default to writing no comments. Add one when a future reader would otherwise be confused.
