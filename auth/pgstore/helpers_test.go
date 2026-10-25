package pgstore

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/berkkaradalan/CoreGo/auth"
)

func TestMapError(t *testing.T) {
	duplicate := &pgconn.PgError{
		Code:    "23505",
		Message: `duplicate key value violates unique constraint "users_email_key"`,
	}
	foreignKey := &pgconn.PgError{
		Code:    "23503",
		Message: `insert or update on table "accounts" violates foreign key constraint`,
	}

	tests := []struct {
		name string
		err  error
		// want is the expected mapped error compared with errors.Is;
		// nil expects nil.
		want error
		// unchanged requires the exact same error instance back, for
		// cases that must not be wrapped or replaced.
		unchanged bool
	}{
		{name: "nil maps to nil", err: nil, want: nil, unchanged: true},
		{name: "pgx.ErrNoRows maps to auth.ErrNotFound", err: pgx.ErrNoRows, want: auth.ErrNotFound},
		{name: "wrapped pgx.ErrNoRows maps to auth.ErrNotFound", err: fmt.Errorf("query users: %w", pgx.ErrNoRows), want: auth.ErrNotFound},
		{name: "SQLSTATE 23505 maps to auth.ErrDuplicate", err: duplicate, want: auth.ErrDuplicate},
		{name: "wrapped SQLSTATE 23505 maps to auth.ErrDuplicate", err: fmt.Errorf("insert user: %w", duplicate), want: auth.ErrDuplicate},
		{name: "other SQLSTATE is returned unchanged", err: foreignKey, want: foreignKey, unchanged: true},
		{name: "canceled context is returned unchanged", err: contextCanceled(), want: nil, unchanged: true},
		{name: "generic error is returned unchanged", err: errors.New("connection refused"), want: nil, unchanged: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError(tt.err)

			if tt.want == nil {
				if tt.unchanged {
					// The original error must come back untouched.
					if got != tt.err {
						t.Fatalf("mapError(%v) = %v, want the identical error", tt.err, got)
					}
					return
				}
				if got != nil {
					t.Fatalf("mapError(%v) = %v, want nil", tt.err, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("mapError(%v) = %v, want %v", tt.err, got, tt.want)
			}
			if tt.unchanged && got != tt.err {
				t.Fatalf("mapError(%v) = %v, want the identical error", tt.err, got)
			}
		})
	}
}

// contextCanceled returns an error that is neither pgx.ErrNoRows nor a
// PgError, mimicking a query aborted by context cancellation.
func contextCanceled() error {
	return errors.New("context canceled")
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIDFormat(t *testing.T) {
	for i := 0; i < 1000; i++ {
		id := newID()
		if !uuidPattern.MatchString(id) {
			t.Fatalf("newID() = %q, want a lowercase UUIDv4-formatted string", id)
		}
	}
}

func TestNewIDUniqueness(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := newID()
		if _, dup := seen[id]; dup {
			t.Fatalf("newID() produced a duplicate id %q after %d draws", id, i)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("generated %d unique ids, want %d", len(seen), n)
	}
}

func TestNormalizeListOptions(t *testing.T) {
	tests := []struct {
		name       string
		opts       auth.ListOptions
		wantLimit  int
		wantOffset int
	}{
		{
			name:       "zero values select the defaults",
			opts:       auth.ListOptions{},
			wantLimit:  defaultListLimit,
			wantOffset: 0,
		},
		{
			name:       "negative limit falls back to the default",
			opts:       auth.ListOptions{Limit: -25},
			wantLimit:  defaultListLimit,
			wantOffset: 0,
		},
		{
			name:       "negative offset is clamped to zero",
			opts:       auth.ListOptions{Limit: 10, Offset: -3},
			wantLimit:  10,
			wantOffset: 0,
		},
		{
			name:       "both negative fall back to the defaults",
			opts:       auth.ListOptions{Limit: -1, Offset: -1},
			wantLimit:  defaultListLimit,
			wantOffset: 0,
		},
		{
			name:       "positive values pass through unchanged",
			opts:       auth.ListOptions{Limit: 7, Offset: 14},
			wantLimit:  7,
			wantOffset: 14,
		},
		{
			name:       "limit of one is preserved",
			opts:       auth.ListOptions{Limit: 1},
			wantLimit:  1,
			wantOffset: 0,
		},
		{
			name:       "large limits are not capped",
			opts:       auth.ListOptions{Limit: 100000},
			wantLimit:  100000,
			wantOffset: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLimit, gotOffset := normalizeListOptions(tt.opts)
			if gotLimit != tt.wantLimit {
				t.Errorf("normalizeListOptions(%+v) limit = %d, want %d", tt.opts, gotLimit, tt.wantLimit)
			}
			if gotOffset != tt.wantOffset {
				t.Errorf("normalizeListOptions(%+v) offset = %d, want %d", tt.opts, gotOffset, tt.wantOffset)
			}
		})
	}
}

func TestUTCOrNow(t *testing.T) {
	now := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

	t.Run("zero time is replaced with now", func(t *testing.T) {
		got := utcOrNow(time.Time{}, now)
		if !got.Equal(now) {
			t.Fatalf("utcOrNow(zero, now) = %v, want %v", got, now)
		}
	})

	t.Run("nonzero time is converted to UTC", func(t *testing.T) {
		fixed := time.FixedZone("test", 2*60*60)
		local := time.Date(2026, 1, 2, 15, 4, 5, 0, fixed)
		got := utcOrNow(local, now)
		if got.Location() != time.UTC {
			t.Fatalf("utcOrNow returned %v with location %v, want UTC", got, got.Location())
		}
		want := time.Date(2026, 1, 2, 13, 4, 5, 0, time.UTC)
		if !got.Equal(want) {
			t.Fatalf("utcOrNow(%v) = %v, want %v", local, got, want)
		}
	})

	t.Run("sub-microsecond precision is truncated", func(t *testing.T) {
		precise := time.Date(2026, 1, 2, 15, 4, 5, 123456789, time.UTC)
		got := utcOrNow(precise, now)
		want := time.Date(2026, 1, 2, 15, 4, 5, 123456000, time.UTC)
		if !got.Equal(want) {
			t.Fatalf("utcOrNow(%v) = %v, want %v truncated to microseconds", precise, got, want)
		}
	})
}

func TestNormalizeTime(t *testing.T) {
	fixed := time.FixedZone("plus3", 3*60*60)
	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"zero stays zero", time.Time{}, time.Time{}},
		{"utc whole microseconds unchanged", time.Date(2026, 5, 6, 7, 8, 9, 123456000, time.UTC), time.Date(2026, 5, 6, 7, 8, 9, 123456000, time.UTC)},
		{"nanoseconds truncated, not rounded", time.Date(2026, 5, 6, 7, 8, 9, 123456999, time.UTC), time.Date(2026, 5, 6, 7, 8, 9, 123456000, time.UTC)},
		{"zone converted to utc", time.Date(2026, 5, 6, 10, 8, 9, 500, fixed), time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeTime(tt.in)
			if !got.Equal(tt.want) {
				t.Fatalf("normalizeTime(%v) = %v, want %v", tt.in, got, tt.want)
			}
			if !got.IsZero() && got.Location() != time.UTC {
				t.Fatalf("normalizeTime(%v) location = %v, want UTC", tt.in, got.Location())
			}
		})
	}

	if normalizeTimePtr(nil) != nil {
		t.Fatal("normalizeTimePtr(nil) != nil")
	}
	in := time.Date(2026, 5, 6, 7, 8, 9, 999, fixed)
	got := normalizeTimePtr(&in)
	if got == &in || !got.Equal(normalizeTime(in)) {
		t.Fatalf("normalizeTimePtr(%v) = %v, want a new pointer to %v", in, got, normalizeTime(in))
	}
}

// TestNormalizeTimePreservesOrder guards the property the auth service
// relies on when it compares a token's CreatedAt with the user's
// CreatedAt: truncation may make two instants equal, but never swaps
// their order.
func TestNormalizeTimePreservesOrder(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	offsets := []time.Duration{0, 1, 499, 500, 501, 999, 1000, 1001, 1999, 2000, time.Second - 1}
	for _, a := range offsets {
		for _, b := range offsets {
			ta, tb := base.Add(a), base.Add(b)
			na, nb := normalizeTime(ta), normalizeTime(tb)
			if !ta.Before(tb) && na.Before(nb) {
				t.Fatalf("order flipped: %v !< %v but normalized %v < %v", ta, tb, na, nb)
			}
			if !ta.After(tb) && na.After(nb) {
				t.Fatalf("order flipped: %v !> %v but normalized %v > %v", ta, tb, na, nb)
			}
		}
	}
}

func TestExpiryCutoff(t *testing.T) {
	fixed := time.FixedZone("minus5", -5*60*60)
	tests := []struct {
		name   string
		before time.Time
		want   time.Time
	}{
		{"whole microsecond unchanged", time.Date(2026, 1, 1, 0, 0, 0, 7000, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 7000, time.UTC)},
		{"one nanosecond over rounds up", time.Date(2026, 1, 1, 0, 0, 0, 7001, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 8000, time.UTC)},
		{"just below the next microsecond rounds up", time.Date(2026, 1, 1, 0, 0, 0, 7999, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 8000, time.UTC)},
		{"zone converted to utc", time.Date(2025, 12, 31, 19, 0, 0, 1, fixed), time.Date(2026, 1, 1, 0, 0, 0, 1000, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expiryCutoff(tt.before)
			if !got.Equal(tt.want) || got.Location() != time.UTC {
				t.Fatalf("expiryCutoff(%v) = %v, want %v in UTC", tt.before, got, tt.want)
			}
		})
	}

	// For every stored (microsecond aligned) value v: v < before must
	// equal v < expiryCutoff(before).
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for v := time.Duration(0); v <= 4*time.Microsecond; v += time.Microsecond {
		for b := time.Duration(0); b <= 4*time.Microsecond; b += 250 * time.Nanosecond {
			stored, before := base.Add(v), base.Add(b)
			if stored.Before(before) != stored.Before(expiryCutoff(before)) {
				t.Fatalf("stored %v, before %v: exact %v, cutoff %v", stored, before, stored.Before(before), stored.Before(expiryCutoff(before)))
			}
		}
	}
}
