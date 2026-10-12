---
name: go-style
description: Go coding style — explicit two-line error handling, snake_case JSON tags that match field and column names, no abbreviations in names, and when to name return values. Use when writing, editing or reviewing any Go code (*.go).
---

# Go Coding Style

## Error Handling

Always use explicit two-line error handling. Never use inline assignment.

**Correct:**
```go
result, err := someFunction()
if err != nil {
    return err
}
```

**Incorrect:**
```go
if err := someFunction(); err != nil {
    return err
}
```

The two-line style is easier to read but also easier to debug — you can set breakpoints on the
assignment and the check separately.

## JSON Tags

JSON tags must:
- Match the Go field name in `snake_case`
- Match the corresponding database column name where applicable

```go
// Correct
DriveMinutes int `json:"drive_minutes"`

// Incorrect — abbreviated
DriveMinutes int `json:"drive_min"`
```

## Naming

- No abbreviations in exported identifiers — and none in query parameters either
  (`latitude`, not `lat`)
- Use full descriptive names: `DistanceKilometres` not `DistKm`
- Package-level functions should be self-documenting
- A field is named by what it holds, never by its type alone

## Named Return Values

Use named return values when the types alone don't tell the story.

**Always name when:**
- A return type is ambiguous (e.g. `bool` — bool of what?)
- There are 2+ return values of the same type
- There are 3+ total return values

**Skip naming when obvious:**
- Single error: `func Validate() error`
- Type + error where the function name implies the type: `func NewStore() (*Store, error)`

```go
// Correct — the bool and the strings are ambiguous without names
func (c *Client) Search(ctx context.Context, text string) (position geo.Point, label string, found bool, err error)
```
