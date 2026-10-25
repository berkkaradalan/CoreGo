package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoTask is the typed document the suite stores.
type mongoTask struct {
	ID       bson.ObjectID `bson:"_id,omitempty"`
	Title    string        `bson:"title"`
	Done     bool          `bson:"done"`
	Priority int           `bson:"priority"`
}

// seedTasks inserts tasks with priorities 1..n (titles "task-1"...).
func seedTasks(t *testing.T, db *MongoDB, coll string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		task := mongoTask{Title: "task-" + string(rune('0'+i)), Priority: i, Done: i%2 == 0}
		if _, err := db.InsertOne(context.Background(), coll, task); err != nil {
			t.Fatalf("seed InsertOne: %v", err)
		}
	}
}

func TestMongoIntegrationConnect(t *testing.T) {
	url := mongoTestURL(t)

	t.Run("explicit database", func(t *testing.T) {
		cfg := MongoConfig{URL: url, Database: mongoTestDatabase}
		db, err := NewMongoDB(cfg)
		if err != nil {
			t.Fatalf("NewMongoDB: %v", err)
		}
		defer db.Close()

		if db.Name() != mongoTestDatabase || db.Database().Name() != mongoTestDatabase {
			t.Fatalf("Name = %q, Database().Name() = %q, want %q", db.Name(), db.Database().Name(), mongoTestDatabase)
		}
		if db.Client() == nil {
			t.Fatal("Client() = nil")
		}
		if got := db.Collection("x").Database().Name(); got != mongoTestDatabase {
			t.Fatalf("Collection database = %q, want %q", got, mongoTestDatabase)
		}
		if err := db.Ping(context.Background()); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})

	t.Run("default database leaves config untouched", func(t *testing.T) {
		if strings.Count(url, "/") != 2 {
			t.Skip("COREGO_TEST_MONGO_URL already names a database")
		}
		cfg := MongoConfig{URL: url}
		db, err := NewMongoDB(cfg)
		if err != nil {
			t.Fatalf("NewMongoDB: %v", err)
		}
		defer db.Close()
		if db.Name() != DefaultMongoDatabase {
			t.Fatalf("Name = %q, want %q", db.Name(), DefaultMongoDatabase)
		}
		if cfg.Database != "" {
			t.Fatalf("cfg.Database mutated to %q", cfg.Database)
		}
	})

	t.Run("database from url path", func(t *testing.T) {
		if strings.Count(url, "/") != 2 || strings.Contains(url, "?") {
			t.Skip("COREGO_TEST_MONGO_URL has a path or options")
		}
		db, err := NewMongoDB(MongoConfig{URL: url + "/corego_database_url_test"})
		if err != nil {
			t.Fatalf("NewMongoDB: %v", err)
		}
		defer db.Close()
		if db.Name() != "corego_database_url_test" {
			t.Fatalf("Name = %q, want the database from the URL", db.Name())
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		db, err := NewMongoDB(MongoConfig{URL: url, Database: mongoTestDatabase})
		if err != nil {
			t.Fatalf("NewMongoDB: %v", err)
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
	})
}

func TestMongoIntegrationInsertOne(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()

	id, err := db.InsertOne(ctx, coll, mongoTask{Title: "generated id"})
	if err != nil {
		t.Fatalf("InsertOne: %v", err)
	}
	oid, ok := id.(bson.ObjectID)
	if !ok || oid.IsZero() {
		t.Fatalf("InsertOne id = %#v (%T), want a non-zero bson.ObjectID", id, id)
	}

	var got mongoTask
	if err := db.FindOne(ctx, coll, bson.M{"_id": oid}, &got); err != nil {
		t.Fatalf("FindOne by returned id: %v", err)
	}
	if got.Title != "generated id" {
		t.Fatalf("FindOne title = %q", got.Title)
	}

	custom, err := db.InsertOne(ctx, coll, bson.M{"_id": "my-key", "title": "custom id"})
	if err != nil {
		t.Fatalf("InsertOne custom id: %v", err)
	}
	if custom != "my-key" {
		t.Fatalf("InsertOne custom id = %#v, want %q", custom, "my-key")
	}

	_, err = db.InsertOne(ctx, coll, bson.M{"_id": "my-key"})
	if !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("duplicate InsertOne error = %v, want a duplicate key error", err)
	}
}

func TestMongoIntegrationFindOne(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()
	seedTasks(t, db, coll, 3)

	var task mongoTask
	if err := db.FindOne(ctx, coll, bson.M{"priority": 2}, &task); err != nil {
		t.Fatalf("FindOne: %v", err)
	}
	if task.Title != "task-2" || !task.Done || task.ID.IsZero() {
		t.Fatalf("FindOne = %+v", task)
	}

	var generic bson.M
	if err := db.FindOne(ctx, coll, nil, &generic); err != nil {
		t.Fatalf("FindOne with nil filter: %v", err)
	}
	if generic["title"] == nil {
		t.Fatalf("FindOne with nil filter decoded %v", generic)
	}

	err := db.FindOne(ctx, coll, bson.M{"priority": 99}, &task)
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("FindOne miss = %v, want ErrNotFound and mongo.ErrNoDocuments", err)
	}

	emptyColl := testCollection(t, db)
	if err := db.FindOne(ctx, emptyColl, nil, &task); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindOne on empty collection = %v, want ErrNotFound", err)
	}
}

func TestMongoIntegrationFind(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()
	seedTasks(t, db, coll, 5)

	t.Run("typed slice with filter", func(t *testing.T) {
		var open []mongoTask
		if err := db.Find(ctx, coll, bson.M{"done": false}, &open); err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(open) != 3 {
			t.Fatalf("Find returned %d open tasks, want 3", len(open))
		}
		for _, task := range open {
			if task.Done || task.ID.IsZero() || task.Title == "" {
				t.Fatalf("Find decoded %+v", task)
			}
		}
	})

	t.Run("options sort skip limit", func(t *testing.T) {
		var page []mongoTask
		opts := options.Find().
			SetSort(bson.D{{Key: "priority", Value: -1}}).
			SetSkip(1).
			SetLimit(2)
		if err := db.Find(ctx, coll, nil, &page, opts); err != nil {
			t.Fatalf("Find with options: %v", err)
		}
		if len(page) != 2 || page[0].Priority != 4 || page[1].Priority != 3 {
			t.Fatalf("Find page = %+v, want priorities 4 and 3", page)
		}
	})

	t.Run("multiple options merge", func(t *testing.T) {
		var page []mongoTask
		err := db.Find(ctx, coll, nil, &page,
			options.Find().SetSort(bson.D{{Key: "priority", Value: 1}}),
			options.Find().SetLimit(1))
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(page) != 1 || page[0].Priority != 1 {
			t.Fatalf("Find page = %+v, want priority 1 only", page)
		}
	})

	t.Run("projection into maps", func(t *testing.T) {
		var docs []bson.M
		opts := options.Find().SetProjection(bson.M{"_id": 0, "title": 1})
		if err := db.Find(ctx, coll, bson.M{"priority": 1}, &docs, opts); err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(docs) != 1 || len(docs[0]) != 1 || docs[0]["title"] != "task-1" {
			t.Fatalf("Find projection = %v", docs)
		}
	})

	t.Run("no match gives empty non-nil slice", func(t *testing.T) {
		var none []mongoTask
		if err := db.Find(ctx, coll, bson.M{"priority": 99}, &none); err != nil {
			t.Fatalf("Find: %v", err)
		}
		if none == nil || len(none) != 0 {
			t.Fatalf("Find miss = %#v, want an empty non-nil slice", none)
		}
	})

	t.Run("previous contents are replaced", func(t *testing.T) {
		out := []mongoTask{{Title: "stale"}, {Title: "stale"}, {Title: "stale"}, {Title: "stale"}}
		if err := db.Find(ctx, coll, bson.M{"priority": 1}, &out); err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(out) != 1 || out[0].Title != "task-1" {
			t.Fatalf("Find into a used slice = %+v", out)
		}
		if err := db.Find(ctx, coll, bson.M{"priority": 99}, &out); err != nil {
			t.Fatalf("Find: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("Find miss into a used slice = %+v, want empty", out)
		}
	})

	t.Run("invalid out", func(t *testing.T) {
		var one mongoTask
		if err := db.Find(ctx, coll, nil, &one); err == nil {
			t.Fatal("Find into a struct pointer succeeded")
		}
		var tasks []mongoTask
		if err := db.Find(ctx, coll, nil, tasks); err == nil {
			t.Fatal("Find into a slice value succeeded")
		}
	})

	t.Run("decode error", func(t *testing.T) {
		var wrong []struct {
			Title int `bson:"title"`
		}
		if err := db.Find(ctx, coll, nil, &wrong); err == nil {
			t.Fatal("Find decoding strings into ints succeeded")
		}
	})
}

func TestMongoIntegrationCount(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()

	n, err := db.Count(ctx, coll, nil)
	if err != nil || n != 0 {
		t.Fatalf("Count on empty collection = (%d, %v), want (0, nil)", n, err)
	}

	seedTasks(t, db, coll, 5)
	tests := []struct {
		name   string
		filter any
		want   int64
	}{
		{"nil filter", nil, 5},
		{"empty filter", bson.M{}, 5},
		{"done", bson.M{"done": true}, 2},
		{"range", bson.M{"priority": bson.M{"$gte": 3}}, 3},
		{"no match", bson.M{"priority": 99}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := db.Count(ctx, coll, tt.filter)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Count = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMongoIntegrationUpdate(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()
	seedTasks(t, db, coll, 5)

	if err := db.UpdateOne(ctx, coll, bson.M{"priority": 1}, bson.M{"$set": bson.M{"title": "renamed"}}); err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	var task mongoTask
	if err := db.FindOne(ctx, coll, bson.M{"priority": 1}, &task); err != nil || task.Title != "renamed" {
		t.Fatalf("after UpdateOne = (%+v, %v)", task, err)
	}

	// Matching without changing anything is still a success.
	if err := db.UpdateOne(ctx, coll, bson.M{"priority": 1}, bson.M{"$set": bson.M{"title": "renamed"}}); err != nil {
		t.Fatalf("no-op UpdateOne: %v", err)
	}

	err := db.UpdateOne(ctx, coll, bson.M{"priority": 99}, bson.M{"$set": bson.M{"title": "x"}})
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("UpdateOne miss = %v, want ErrNotFound and mongo.ErrNoDocuments", err)
	}

	n, err := db.UpdateMany(ctx, coll, bson.M{"done": false}, bson.M{"$set": bson.M{"done": true}})
	if err != nil || n != 3 {
		t.Fatalf("UpdateMany = (%d, %v), want (3, nil)", n, err)
	}
	// UpdateMany reports matched documents, including unchanged ones.
	n, err = db.UpdateMany(ctx, coll, bson.M{}, bson.M{"$set": bson.M{"done": true}})
	if err != nil || n != 5 {
		t.Fatalf("UpdateMany all = (%d, %v), want (5, nil)", n, err)
	}
	n, err = db.UpdateMany(ctx, coll, bson.M{"priority": 99}, bson.M{"$set": bson.M{"done": false}})
	if err != nil || n != 0 {
		t.Fatalf("UpdateMany miss = (%d, %v), want (0, nil)", n, err)
	}

	if err := db.UpdateOne(ctx, coll, bson.M{"priority": 1}, bson.M{"title": "no operator"}); err == nil {
		t.Fatal("UpdateOne with a replacement document succeeded, want the driver's error")
	}
}

func TestMongoIntegrationDelete(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()
	seedTasks(t, db, coll, 5)

	if err := db.DeleteOne(ctx, coll, bson.M{"priority": 1}); err != nil {
		t.Fatalf("DeleteOne: %v", err)
	}
	err := db.DeleteOne(ctx, coll, bson.M{"priority": 1})
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("second DeleteOne = %v, want ErrNotFound and mongo.ErrNoDocuments", err)
	}

	n, err := db.DeleteMany(ctx, coll, bson.M{"done": true})
	if err != nil || n != 2 {
		t.Fatalf("DeleteMany = (%d, %v), want (2, nil)", n, err)
	}
	n, err = db.DeleteMany(ctx, coll, bson.M{"done": true})
	if err != nil || n != 0 {
		t.Fatalf("DeleteMany miss = (%d, %v), want (0, nil)", n, err)
	}
	n, err = db.DeleteMany(ctx, coll, bson.M{})
	if err != nil || n != 2 {
		t.Fatalf("DeleteMany all = (%d, %v), want (2, nil)", n, err)
	}
}

func TestMongoIntegrationWritesRejectNilFilter(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()
	seedTasks(t, db, coll, 2)

	set := bson.M{"$set": bson.M{"done": true}}
	if err := db.UpdateOne(ctx, coll, nil, set); !errors.Is(err, mongo.ErrNilDocument) {
		t.Fatalf("UpdateOne(nil filter) = %v, want mongo.ErrNilDocument", err)
	}
	if _, err := db.UpdateMany(ctx, coll, nil, set); !errors.Is(err, mongo.ErrNilDocument) {
		t.Fatalf("UpdateMany(nil filter) = %v, want mongo.ErrNilDocument", err)
	}
	if err := db.DeleteOne(ctx, coll, nil); !errors.Is(err, mongo.ErrNilDocument) {
		t.Fatalf("DeleteOne(nil filter) = %v, want mongo.ErrNilDocument", err)
	}
	if _, err := db.DeleteMany(ctx, coll, nil); !errors.Is(err, mongo.ErrNilDocument) {
		t.Fatalf("DeleteMany(nil filter) = %v, want mongo.ErrNilDocument", err)
	}
	if n, err := db.Count(ctx, coll, nil); err != nil || n != 2 {
		t.Fatalf("documents after rejected writes = (%d, %v), want (2, nil)", n, err)
	}
}

func TestMongoIntegrationEscapeHatches(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	ctx := context.Background()
	seedTasks(t, db, coll, 4)

	cursor, err := db.Collection(coll).Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$done"}, {Key: "total", Value: bson.D{{Key: "$sum", Value: "$priority"}}}}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	var groups []struct {
		Done  bool `bson:"_id"`
		Total int  `bson:"total"`
	}
	if err := cursor.All(ctx, &groups); err != nil {
		t.Fatalf("cursor.All: %v", err)
	}
	if len(groups) != 2 || groups[0].Total != 1+3 || groups[1].Total != 2+4 {
		t.Fatalf("Aggregate groups = %+v", groups)
	}

	names, err := db.Database().ListCollectionNames(ctx, bson.M{"name": coll})
	if err != nil || len(names) != 1 {
		t.Fatalf("ListCollectionNames = (%v, %v), want the test collection", names, err)
	}
}

func TestMongoIntegrationHonorsContext(t *testing.T) {
	db := newTestMongo(t)
	coll := testCollection(t, db)
	seedTasks(t, db, coll, 1)
	ctx := canceledContext()

	var task mongoTask
	var tasks []mongoTask
	set := bson.M{"$set": bson.M{"done": true}}
	calls := map[string]func() error{
		"Ping":      func() error { return db.Ping(ctx) },
		"InsertOne": func() error { _, err := db.InsertOne(ctx, coll, mongoTask{Title: "x"}); return err },
		"FindOne":   func() error { return db.FindOne(ctx, coll, nil, &task) },
		"Find":      func() error { return db.Find(ctx, coll, nil, &tasks) },
		"Count":     func() error { _, err := db.Count(ctx, coll, nil); return err },
		"UpdateOne": func() error { return db.UpdateOne(ctx, coll, bson.M{}, set) },
		"UpdateMany": func() error {
			_, err := db.UpdateMany(ctx, coll, bson.M{}, set)
			return err
		},
		"DeleteOne": func() error { return db.DeleteOne(ctx, coll, bson.M{}) },
		"DeleteMany": func() error {
			_, err := db.DeleteMany(ctx, coll, bson.M{})
			return err
		},
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

	// Nothing above may have reached the server.
	if n, err := db.Count(context.Background(), coll, nil); err != nil || n != 1 {
		t.Fatalf("documents after canceled calls = (%d, %v), want (1, nil)", n, err)
	}
	if err := db.FindOne(context.Background(), coll, bson.M{"done": true}, &task); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a canceled update was applied: %v", err)
	}

	deadline, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if err := db.Find(deadline, coll, nil, &tasks); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Find past its deadline = %v, want context.DeadlineExceeded", err)
	}
}
