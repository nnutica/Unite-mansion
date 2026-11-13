package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// memLog mimics the SQL: order by (occurred_at, id) descending and return
// rows strictly after the cursor.
type memLog []LogEntry

func (m memLog) List(_ context.Context, q ListQuery) ([]LogEntry, error) {
	rows := append([]LogEntry(nil), m...)
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].OccurredAt.Equal(rows[j].OccurredAt) {
			return rows[i].OccurredAt.After(rows[j].OccurredAt)
		}
		return rows[i].ID > rows[j].ID
	})
	var out []LogEntry
	for _, e := range rows {
		if q.After != nil {
			newer := e.OccurredAt.After(q.After.OccurredAt)
			sameButNotBefore := e.OccurredAt.Equal(q.After.OccurredAt) && e.ID >= q.After.ID
			if newer || sameButNotBefore {
				continue
			}
		}
		out = append(out, e)
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

func pageAll(t *testing.T, log memLog, limit int) []string {
	t.Helper()
	var seen []string
	var cursor *string
	for range 100 {
		p, err := ListPage(context.Background(), log, nil, cursor, limit)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.Items {
			seen = append(seen, e.ID)
		}
		if p.NextCursor == nil {
			return seen
		}
		cursor = p.NextCursor
	}
	t.Fatal("paging did not terminate")
	return nil
}

func TestListPageWithEqualTimestamps(t *testing.T) {
	// Every entry has the same timestamp (several events in one request, or
	// a frozen test clock): the old time-only cursor looped forever here.
	at := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	var log memLog
	for range 7 {
		log = append(log, LogEntry{ID: uuid.NewString(), OccurredAt: at})
	}
	for _, limit := range []int{1, 2, 3, 7, 50} {
		seen := pageAll(t, log, limit)
		if len(seen) != 7 {
			t.Fatalf("limit %d: paged %d entries, want 7", limit, len(seen))
		}
		uniq := map[string]bool{}
		for _, id := range seen {
			uniq[id] = true
		}
		if len(uniq) != 7 {
			t.Fatalf("limit %d: duplicates in %v", limit, seen)
		}
	}
}

func TestListPageMixedTimestamps(t *testing.T) {
	base := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	var log memLog
	for i, s := range []int{5, 4, 3, 3, 3, 2, 1} {
		log = append(log, LogEntry{ID: fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i), OccurredAt: base.Add(time.Duration(s) * time.Minute)})
	}
	if seen := pageAll(t, log, 2); len(seen) != 7 {
		t.Fatalf("paged %v", seen)
	}
}

func TestListPageLimitsAndCursor(t *testing.T) {
	var log memLog
	for i := range 300 {
		log = append(log, LogEntry{ID: uuid.NewString(), OccurredAt: time.Unix(int64(1000-i), 0)})
	}
	p, _ := ListPage(context.Background(), log, nil, nil, 0)
	if len(p.Items) != defaultPageSize || p.NextCursor == nil {
		t.Fatalf("default page = %d items", len(p.Items))
	}
	p, _ = ListPage(context.Background(), log, nil, nil, 1000)
	if len(p.Items) != maxPageSize {
		t.Fatalf("max page = %d items", len(p.Items))
	}

	bad := "not-a-cursor"
	if _, err := ListPage(context.Background(), log, nil, &bad, 10); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
	c := Cursor{OccurredAt: time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC), ID: uuid.NewString()}
	back, err := DecodeCursor(EncodeCursor(c))
	if err != nil || !back.OccurredAt.Equal(c.OccurredAt) || back.ID != c.ID {
		t.Fatalf("round trip = %+v %v", back, err)
	}
}
