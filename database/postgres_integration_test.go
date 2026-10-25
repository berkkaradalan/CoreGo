package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// pgTask maps the test table through `db` tags and field names.
type pgTask struct {
	ID        int64     `db:"id"`
	Title     string    `db:"title"`
	Done      bool      // matched by field name
	Priority  int       `db:"priority"`
	CreatedAt time.Time // created_at, matched by name with underscores ignored
}

// pgTaskSummary has fewer fields than the table; queries select only
// these columns.
type pgTaskSummary struct {
	Title string `db:"title"`
	Notes string `db:"-"`
}

// seedRows inserts tasks with priorities 1..n (titles "task-1"...).
func seedRows(t *testing.T, db *PostgresDB, table string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		if _, err := db.Exec(context.Background(),
			`INSERT INTO `+table+` (title, done, priority) VALUES ($1, $2, $3)`,
			fmt.Sprintf("task-%d", i), i%2 == 0, i); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func TestPostgresIntegrationConnect(t *testing.T) {
	url := postgresTestURL(t)
	db, err := NewPostgresDB(PostgresConfig{URL: url})
	if err != nil {
		t.Fatalf("NewPostgresDB: %v", err)
	}
	if db.Pool() == nil {
		t.Fatal("Pool() = nil")
	}
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := db.Ping(context.Background()); err == nil {
		t.Fatal("Ping after Close succeeded")
	}
}

func TestPostgresIntegrationUnknownDatabase(t *testing.T) {
	url := postgresTestURL(t)
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	bad := fmt.Sprintf("postgres://%s:%s@%s:%d/corego_database_that_does_not_exist",
		cfg.User, cfg.Password, cfg.Host, cfg.Port)
	db, err := NewPostgresDB(PostgresConfig{URL: bad})
	if err == nil || db != nil {
		t.Fatalf("NewPostgresDB(unknown database) = (%v, %v), want (nil, error)", db, err)
	}
}

func TestPostgresIntegrationExec(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	ctx := context.Background()

	n, err := db.Exec(ctx, `INSERT INTO `+table+` (title) VALUES ($1), ($2)`, "a", "b")
	if err != nil || n != 2 {
		t.Fatalf("insert = (%d, %v), want (2, nil)", n, err)
	}
	n, err = db.Exec(ctx, `UPDATE `+table+` SET done = TRUE WHERE title = $1`, "a")
	if err != nil || n != 1 {
		t.Fatalf("update = (%d, %v), want (1, nil)", n, err)
	}
	n, err = db.Exec(ctx, `UPDATE `+table+` SET done = TRUE WHERE title = $1`, "missing")
	if err != nil || n != 0 {
		t.Fatalf("update miss = (%d, %v), want (0, nil)", n, err)
	}
	n, err = db.Exec(ctx, `DELETE FROM `+table)
	if err != nil || n != 2 {
		t.Fatalf("delete = (%d, %v), want (2, nil)", n, err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO `+table+` (title) VALUES (NULL)`); err == nil {
		t.Fatal("NOT NULL violation succeeded")
	}
	if _, err := db.Exec(ctx, `SELEC 1`); err == nil {
		t.Fatal("syntax error succeeded")
	}
}

func TestPostgresIntegrationQuery(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	ctx := context.Background()
	seedRows(t, db, table, 3)

	rows, err := db.Query(ctx, `SELECT title FROM `+table+` ORDER BY priority`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	titles, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("CollectRows: %v", err)
	}
	if len(titles) != 3 || titles[0] != "task-1" || titles[2] != "task-3" {
		t.Fatalf("titles = %v", titles)
	}
}

func TestPostgresIntegrationQueryMaps(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	ctx := context.Background()
	seedRows(t, db, table, 2)

	maps, err := db.QueryMaps(ctx, `SELECT title, done, priority FROM `+table+` ORDER BY priority`)
	if err != nil {
		t.Fatalf("QueryMaps: %v", err)
	}
	if len(maps) != 2 {
		t.Fatalf("QueryMaps returned %d rows, want 2", len(maps))
	}
	if maps[0]["title"] != "task-1" || maps[0]["done"] != false || maps[0]["priority"] != int32(1) {
		t.Fatalf("QueryMaps first row = %v", maps[0])
	}

	returning, err := db.QueryMaps(ctx, `INSERT INTO `+table+` (title) VALUES ($1) RETURNING id`, "returned")
	if err != nil || len(returning) != 1 || returning[0]["id"] == nil {
		t.Fatalf("QueryMaps RETURNING = (%v, %v)", returning, err)
	}

	none, err := db.QueryMaps(ctx, `SELECT title FROM `+table+` WHERE priority = $1`, 99)
	if err != nil {
		t.Fatalf("QueryMaps miss: %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("QueryMaps miss = %#v, want an empty non-nil slice", none)
	}

	if _, err := db.QueryMaps(ctx, `SELECT nope FROM `+table); err == nil {
		t.Fatal("QueryMaps on an unknown column succeeded")
	}
}

func TestPostgresIntegrationQueryAll(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	ctx := context.Background()
	seedRows(t, db, table, 4)

	t.Run("struct by tag and field name", func(t *testing.T) {
		tasks, err := QueryAll[pgTask](ctx, db,
			`SELECT id, title, done, priority, created_at FROM `+table+` WHERE priority >= $1 ORDER BY priority`, 2)
		if err != nil {
			t.Fatalf("QueryAll: %v", err)
		}
		if len(tasks) != 3 {
			t.Fatalf("QueryAll returned %d rows, want 3", len(tasks))
		}
		first := tasks[0]
		if first.ID == 0 || first.Title != "task-2" || !first.Done || first.Priority != 2 || first.CreatedAt.IsZero() {
			t.Fatalf("QueryAll first row = %+v", first)
		}
	})

	t.Run("ignored field and column subset", func(t *testing.T) {
		got, err := QueryAll[pgTaskSummary](ctx, db, `SELECT title FROM `+table+` ORDER BY priority LIMIT 2`)
		if err != nil {
			t.Fatalf("QueryAll: %v", err)
		}
		if len(got) != 2 || got[1].Title != "task-2" {
			t.Fatalf("QueryAll = %+v", got)
		}
	})

	t.Run("scalar", func(t *testing.T) {
		titles, err := QueryAll[string](ctx, db, `SELECT title FROM `+table+` ORDER BY priority DESC`)
		if err != nil {
			t.Fatalf("QueryAll[string]: %v", err)
		}
		if len(titles) != 4 || titles[0] != "task-4" {
			t.Fatalf("QueryAll[string] = %v", titles)
		}
		times, err := QueryAll[time.Time](ctx, db, `SELECT created_at FROM `+table)
		if err != nil || len(times) != 4 || times[0].IsZero() {
			t.Fatalf("QueryAll[time.Time] = (%v, %v)", times, err)
		}
		nullable, err := QueryAll[sql.NullString](ctx, db, `SELECT NULL::text UNION ALL SELECT 'x'`)
		if err != nil || len(nullable) != 2 || nullable[0].Valid || nullable[1].String != "x" {
			t.Fatalf("QueryAll[sql.NullString] = (%v, %v)", nullable, err)
		}
	})

	t.Run("no rows gives empty non-nil slice", func(t *testing.T) {
		tasks, err := QueryAll[pgTask](ctx, db,
			`SELECT id, title, done, priority, created_at FROM `+table+` WHERE priority = $1`, 99)
		if err != nil {
			t.Fatalf("QueryAll: %v", err)
		}
		if tasks == nil || len(tasks) != 0 {
			t.Fatalf("QueryAll miss = %#v, want an empty non-nil slice", tasks)
		}
	})

	t.Run("unmapped column", func(t *testing.T) {
		_, err := QueryAll[pgTaskSummary](ctx, db, `SELECT title, priority FROM `+table)
		if err == nil {
			t.Fatal("QueryAll with an extra column succeeded")
		}
	})

	t.Run("missing field", func(t *testing.T) {
		_, err := QueryAll[pgTask](ctx, db, `SELECT id, title FROM `+table)
		if err == nil {
			t.Fatal("QueryAll without every struct field succeeded")
		}
	})

	t.Run("sql error", func(t *testing.T) {
		if _, err := QueryAll[pgTask](ctx, db, `SELECT * FROM corego_dbtest_missing_table`); err == nil {
			t.Fatal("QueryAll on a missing table succeeded")
		}
	})
}

func TestPostgresIntegrationQueryOne(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	ctx := context.Background()
	seedRows(t, db, table, 3)

	task, err := QueryOne[pgTask](ctx, db,
		`SELECT id, title, done, priority, created_at FROM `+table+` WHERE title = $1`, "task-2")
	if err != nil {
		t.Fatalf("QueryOne: %v", err)
	}
	if task.Title != "task-2" || task.Priority != 2 || !task.Done {
		t.Fatalf("QueryOne = %+v", task)
	}

	count, err := QueryOne[int64](ctx, db, `SELECT count(*) FROM `+table)
	if err != nil || count != 3 {
		t.Fatalf("QueryOne[int64] count = (%d, %v), want (3, nil)", count, err)
	}

	first, err := QueryOne[string](ctx, db, `SELECT title FROM `+table+` ORDER BY priority DESC`)
	if err != nil || first != "task-3" {
		t.Fatalf("QueryOne with several rows = (%q, %v), want the first row", first, err)
	}

	missing, err := QueryOne[pgTask](ctx, db,
		`SELECT id, title, done, priority, created_at FROM `+table+` WHERE title = $1`, "missing")
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, pgx.ErrNoRows) || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("QueryOne miss = %v, want ErrNotFound, pgx.ErrNoRows and sql.ErrNoRows", err)
	}
	if missing != (pgTask{}) {
		t.Fatalf("QueryOne miss value = %+v, want the zero value", missing)
	}

	_, err = QueryOne[pgTask](ctx, db, `SELECT id FROM `+table+` LIMIT 1`)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("QueryOne with a missing field = %v, want a scan error that is not ErrNotFound", err)
	}

	returned, err := QueryOne[int64](ctx, db, `INSERT INTO `+table+` (title) VALUES ($1) RETURNING id`, "new")
	if err != nil || returned == 0 {
		t.Fatalf("QueryOne RETURNING = (%d, %v)", returned, err)
	}
}

func TestPostgresIntegrationQuerierImplementations(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	ctx := context.Background()
	seedRows(t, db, table, 2)
	query := `SELECT count(*) FROM ` + table

	conn, err := db.Pool().Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	queriers := map[string]Querier{
		"*PostgresDB":   db,
		"*pgxpool.Pool": db.Pool(),
		"*pgx.Conn":     conn.Conn(),
	}
	for name, q := range queriers {
		t.Run(name, func(t *testing.T) {
			n, err := QueryOne[int64](ctx, q, query)
			if err != nil || n != 2 {
				t.Fatalf("QueryOne via %s = (%d, %v), want (2, nil)", name, n, err)
			}
		})
	}

	t.Run("pgx.Tx", func(t *testing.T) {
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			n, err := QueryOne[int64](ctx, tx, query)
			if err != nil || n != 2 {
				return fmt.Errorf("QueryOne via pgx.Tx = (%d, %v), want (2, nil)", n, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostgresIntegrationWithTx(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	ctx := context.Background()

	count := func(t *testing.T) int64 {
		t.Helper()
		n, err := QueryOne[int64](ctx, db, `SELECT count(*) FROM `+table)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	insert := func(tx pgx.Tx, title string) error {
		_, err := tx.Exec(ctx, `INSERT INTO `+table+` (title) VALUES ($1)`, title)
		return err
	}
	assertPoolIdle := func(t *testing.T) {
		t.Helper()
		if acquired := db.Pool().Stat().AcquiredConns(); acquired != 0 {
			t.Fatalf("%d connections still acquired after WithTx", acquired)
		}
	}

	t.Run("commit", func(t *testing.T) {
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := insert(tx, "commit-1"); err != nil {
				return err
			}
			return insert(tx, "commit-2")
		})
		if err != nil {
			t.Fatalf("WithTx: %v", err)
		}
		if n := count(t); n != 2 {
			t.Fatalf("rows after commit = %d, want 2", n)
		}
		assertPoolIdle(t)
	})

	t.Run("rollback on error", func(t *testing.T) {
		before := count(t)
		sentinel := errors.New("business rule violated")
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := insert(tx, "rolled-back"); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("WithTx = %v, want the callback's error", err)
		}
		if err != sentinel {
			t.Fatalf("WithTx = %v, want the callback's error unchanged", err)
		}
		if n := count(t); n != before {
			t.Fatalf("rows after rollback = %d, want %d", n, before)
		}
		assertPoolIdle(t)
	})

	t.Run("rollback on statement error", func(t *testing.T) {
		before := count(t)
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := insert(tx, "dup"); err != nil {
				return err
			}
			return insert(tx, "dup") // unique violation
		})
		if err == nil {
			t.Fatal("WithTx with a unique violation succeeded")
		}
		if n := count(t); n != before {
			t.Fatalf("rows after failed statement = %d, want %d", n, before)
		}
	})

	t.Run("swallowed statement error fails the commit", func(t *testing.T) {
		before := count(t)
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			_ = insert(tx, "swallow")
			_ = insert(tx, "swallow") // aborts the transaction; error ignored
			return nil
		})
		if err == nil {
			t.Fatal("WithTx committed an aborted transaction")
		}
		if n := count(t); n != before {
			t.Fatalf("rows after aborted transaction = %d, want %d", n, before)
		}
		assertPoolIdle(t)
	})

	t.Run("rollback and re-panic", func(t *testing.T) {
		before := count(t)
		recovered := func() (r any) {
			defer func() { r = recover() }()
			_ = db.WithTx(ctx, func(tx pgx.Tx) error {
				if err := insert(tx, "panicked"); err != nil {
					t.Errorf("insert: %v", err)
				}
				panic("boom")
			})
			return nil
		}()
		if recovered != "boom" {
			t.Fatalf("recovered %v, want the original panic value", recovered)
		}
		if n := count(t); n != before {
			t.Fatalf("rows after panic = %d, want %d", n, before)
		}
		assertPoolIdle(t)
	})

	t.Run("callback may commit itself", func(t *testing.T) {
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := insert(tx, "self-commit"); err != nil {
				return err
			}
			return tx.Commit(ctx)
		})
		if err == nil {
			t.Fatal("WithTx after the callback committed returned nil, want the double-commit error")
		}
		if _, err := QueryOne[int64](ctx, db, `SELECT id FROM `+table+` WHERE title = 'self-commit'`); err != nil {
			t.Fatalf("self-committed row missing: %v", err)
		}
	})

	t.Run("callback error after self rollback is returned unchanged", func(t *testing.T) {
		sentinel := errors.New("gave up")
		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			if err := tx.Rollback(ctx); err != nil {
				return err
			}
			return sentinel
		})
		if err != sentinel {
			t.Fatalf("WithTx = %v, want the callback's error unchanged", err)
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		called := false
		err := db.WithTx(canceledContext(), func(pgx.Tx) error {
			called = true
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WithTx with a canceled context = %v, want context.Canceled", err)
		}
		if called {
			t.Fatal("WithTx ran the callback with a canceled context")
		}
	})

	t.Run("context canceled inside the callback", func(t *testing.T) {
		before := count(t)
		txCtx, cancel := context.WithCancel(ctx)
		err := db.WithTx(txCtx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(txCtx, `INSERT INTO `+table+` (title) VALUES ('canceled')`); err != nil {
				return err
			}
			cancel()
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WithTx = %v, want context.Canceled from the commit", err)
		}
		if n := count(t); n != before {
			t.Fatalf("rows after canceled commit = %d, want %d", n, before)
		}
		assertPoolIdle(t)
	})
}

func TestPostgresIntegrationHonorsContext(t *testing.T) {
	db := newTestPostgres(t)
	table := testTable(t, db)
	seedRows(t, db, table, 1)
	ctx := canceledContext()

	calls := map[string]func() error{
		"Ping": func() error { return db.Ping(ctx) },
		"Exec": func() error {
			_, err := db.Exec(ctx, `UPDATE `+table+` SET done = TRUE`)
			return err
		},
		"Query": func() error {
			rows, err := db.Query(ctx, `SELECT 1`)
			if err == nil {
				rows.Close()
				err = rows.Err()
			}
			return err
		},
		"QueryMaps": func() error { _, err := db.QueryMaps(ctx, `SELECT 1`); return err },
		"QueryAll":  func() error { _, err := QueryAll[int](ctx, db, `SELECT 1`); return err },
		"QueryOne":  func() error { _, err := QueryOne[int](ctx, db, `SELECT 1`); return err },
		"WithTx":    func() error { return db.WithTx(ctx, func(pgx.Tx) error { return nil }) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s with a canceled context = %v, want context.Canceled", name, err)
			}
			if errors.Is(err, ErrNotFound) {
				t.Fatalf("%s cancellation looks like not found", name)
			}
		})
	}

	done, err := QueryOne[bool](context.Background(), db, `SELECT done FROM `+table)
	if err != nil || done {
		t.Fatalf("a canceled update was applied: (%v, %v)", done, err)
	}

	slow, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := db.Exec(slow, `SELECT pg_sleep(5)`); err == nil {
		t.Fatal("pg_sleep(5) finished inside a 100ms deadline")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("deadline not enforced: query ran for %v", elapsed)
	}
}
