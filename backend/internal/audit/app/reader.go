package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidCursor is returned for a cursor this API did not issue.
var ErrInvalidCursor = errors.New("invalid audit log cursor")

// LogEntry is one stored audit row for reading.
type LogEntry struct {
	ID          string
	EventName   string
	AggregateID string
	ActorType   string
	ActorID     *string
	Payload     json.RawMessage
	OccurredAt  time.Time
}

// Cursor is the position after which the next page starts: the
// (occurred_at, id) of the last entry already returned. The pair is unique,
// so paging never skips or repeats rows even when timestamps are equal.
type Cursor struct {
	OccurredAt time.Time
	ID         string
}

type ListQuery struct {
	AggregateID *string
	After       *Cursor // nil = start from the newest
	Limit       int
}

type LogReader interface {
	List(ctx context.Context, q ListQuery) ([]LogEntry, error) // newest first, at most q.Limit rows
}

type Page struct {
	Items      []LogEntry
	NextCursor *string
}

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// ListPage returns one page, newest first. cursor is the NextCursor of the
// previous page, or nil for the first page.
func ListPage(ctx context.Context, r LogReader, aggregateID, cursor *string, limit int) (Page, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	limit = min(limit, maxPageSize)

	q := ListQuery{AggregateID: aggregateID, Limit: limit + 1}
	if cursor != nil {
		c, err := DecodeCursor(*cursor)
		if err != nil {
			return Page{}, err
		}
		q.After = &c
	}
	rows, err := r.List(ctx, q)
	if err != nil {
		return Page{}, fmt.Errorf("list audit logs: %w", err)
	}
	if len(rows) <= limit {
		return Page{Items: rows}, nil
	}
	page := rows[:limit]
	last := page[len(page)-1]
	next := EncodeCursor(Cursor{OccurredAt: last.OccurredAt, ID: last.ID})
	return Page{Items: page, NextCursor: &next}, nil
}

// EncodeCursor makes an opaque, URL-safe cursor.
func EncodeCursor(c Cursor) string {
	raw := c.OccurredAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return Cursor{}, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	if _, err := uuid.Parse(id); err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{OccurredAt: t, ID: id}, nil
}
