package database

import (
	"strings"
	"testing"
	"time"
)

func TestCloseIsNilSafe(t *testing.T) {
	var m *MongoDB
	if err := m.Close(); err != nil {
		t.Fatalf("nil *MongoDB Close = %v, want nil", err)
	}
	var p *PostgresDB
	if err := p.Close(); err != nil {
		t.Fatalf("nil *PostgresDB Close = %v, want nil", err)
	}
}

func TestCloseIsSafeOnZeroValues(t *testing.T) {
	if err := (&MongoDB{}).Close(); err != nil {
		t.Fatalf("zero MongoDB Close = %v, want nil", err)
	}
	if err := (&PostgresDB{}).Close(); err != nil {
		t.Fatalf("zero PostgresDB Close = %v, want nil", err)
	}
}

// Nothing listens on port 1, so the ping fails fast. The constructors
// must report the failure (and release the client/pool, which the race
// detector and goroutine-free return path exercise).
func TestNewMongoDBUnreachableServer(t *testing.T) {
	start := time.Now()
	db, err := newMongoDB(MongoConfig{URL: "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=200"}, 2*time.Second)
	if err == nil || db != nil {
		t.Fatalf("newMongoDB(unreachable) = (%v, %v), want (nil, error)", db, err)
	}
	if !strings.Contains(err.Error(), "ping MongoDB") {
		t.Fatalf("error = %q, want it to mention the ping", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("newMongoDB took %v, want it bounded by the budget", elapsed)
	}
}

func TestNewPostgresDBUnreachableServer(t *testing.T) {
	db, err := newPostgresDB(PostgresConfig{URL: "postgres://user:topsecret@127.0.0.1:1/db?connect_timeout=1"}, 2*time.Second)
	if err == nil || db != nil {
		t.Fatalf("newPostgresDB(unreachable) = (%v, %v), want (nil, error)", db, err)
	}
	if !strings.Contains(err.Error(), "ping PostgreSQL") {
		t.Fatalf("error = %q, want it to mention the ping", err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error %q leaks the password", err)
	}
}

func TestSlicePointer(t *testing.T) {
	var tasks []struct{ Title string }
	var nilSlicePtr *[]int
	notSlice := 3
	tests := []struct {
		name  string
		out   any
		valid bool
	}{
		{"pointer to nil slice", &tasks, true},
		{"pointer to empty slice", &[]int{}, true},
		{"pointer to map slice", &[]map[string]any{}, true},
		{"nil", nil, false},
		{"slice by value", tasks, false},
		{"typed nil pointer", nilSlicePtr, false},
		{"pointer to non-slice", &notSlice, false},
		{"pointer to array", &[2]int{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := slicePointer(tt.out)
			if tt.valid && err != nil {
				t.Fatalf("slicePointer(%T) = %v, want nil", tt.out, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("slicePointer(%T) = nil, want an error", tt.out)
			}
		})
	}
}

func TestFindRejectsNonSlicePointerBeforeQuerying(t *testing.T) {
	// A zero MongoDB has no client: reaching the driver would panic, so
	// a clean error proves the argument is validated first.
	var out struct{}
	err := (&MongoDB{}).Find(t.Context(), "tasks", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "pointer to a slice") {
		t.Fatalf("Find(non-slice) = %v, want a pointer-to-slice error", err)
	}
}

func TestReadFilter(t *testing.T) {
	if got := readFilter(nil); got == nil {
		t.Fatal("readFilter(nil) = nil, want an empty document")
	}
	filter := map[string]any{"done": true}
	if got := readFilter(filter); got == nil {
		t.Fatal("readFilter(filter) = nil, want the filter")
	}
}
