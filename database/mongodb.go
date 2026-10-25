package database

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// connectTimeout bounds the connect+ping performed by the constructors
// and the disconnect performed by MongoDB.Close.
const connectTimeout = 10 * time.Second

// MongoDB is a connected MongoDB client bound to one database. It is
// safe for concurrent use.
//
// Every helper runs with the caller's context, so cancellation and
// deadlines (a canceled HTTP request, a per-query budget) reach the
// server; there is no hidden per-operation timeout. Documents and
// filters are anything the driver can marshal: your own structs with
// `bson` tags, bson.M, bson.D or maps.
type MongoDB struct {
	client *mongo.Client
	db     *mongo.Database

	closeOnce sync.Once
	closeErr  error
}

// NewMongoDB connects to MongoDB, pings the server and returns a handle
// bound to the database resolved from cfg (see MongoConfig.Database).
//
// Connecting and pinging share a 10 second budget. When the ping fails
// the client is disconnected before the error is returned, so nothing
// leaks. cfg is taken by value and never modified. An empty URL is an
// error.
func NewMongoDB(cfg MongoConfig) (*MongoDB, error) {
	return newMongoDB(cfg, connectTimeout)
}

// newMongoDB is NewMongoDB with an adjustable budget for tests.
func newMongoDB(cfg MongoConfig, timeout time.Duration) (*MongoDB, error) {
	name, err := resolveMongoDatabase(cfg)
	if err != nil {
		return nil, err
	}

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.URL))
	if err != nil {
		return nil, fmt.Errorf("database: connect to MongoDB: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		disconnectCtx, cancelDisconnect := context.WithTimeout(context.Background(), timeout)
		defer cancelDisconnect()
		_ = client.Disconnect(disconnectCtx)
		return nil, fmt.Errorf("database: ping MongoDB: %w", err)
	}

	return &MongoDB{client: client, db: client.Database(name)}, nil
}

// Client returns the underlying driver client, for example to start a
// session or a multi-document transaction.
func (m *MongoDB) Client() *mongo.Client {
	return m.client
}

// Database returns the driver handle of the configured database.
func (m *MongoDB) Database() *mongo.Database {
	return m.db
}

// Name returns the resolved database name.
func (m *MongoDB) Name() string {
	return m.db.Name()
}

// Collection returns the driver handle of a collection in the configured
// database: the escape hatch for anything the helpers do not cover
// (aggregations, projections, upserts, bulk writes, indexes).
func (m *MongoDB) Collection(name string) *mongo.Collection {
	return m.db.Collection(name)
}

// Ping checks that the server is reachable.
func (m *MongoDB) Ping(ctx context.Context) error {
	return m.client.Ping(ctx, nil)
}

// Close disconnects the client, waiting at most 10 seconds for in-flight
// operations. It is safe to call on a nil *MongoDB and more than once;
// later calls return the result of the first.
func (m *MongoDB) Close() error {
	if m == nil || m.client == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		m.closeErr = m.client.Disconnect(ctx)
	})
	return m.closeErr
}

// InsertOne inserts document into collection and returns the inserted
// _id exactly as the driver reports it: a bson.ObjectID when the driver
// generated one, otherwise the value of the document's own _id.
func (m *MongoDB) InsertOne(ctx context.Context, collection string, document any) (any, error) {
	res, err := m.Collection(collection).InsertOne(ctx, document)
	if err != nil {
		return nil, err
	}
	return res.InsertedID, nil
}

// FindOne decodes the first document matching filter into out, which
// must be a non-nil pointer (to a struct, a bson.M, ...). A nil filter
// matches every document. When nothing matches it returns an error that
// matches both ErrNotFound and mongo.ErrNoDocuments.
func (m *MongoDB) FindOne(ctx context.Context, collection string, filter any, out any) error {
	err := m.Collection(collection).FindOne(ctx, readFilter(filter)).Decode(out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return notFound(mongo.ErrNoDocuments)
	}
	return err
}

// Find decodes every document matching filter into out, which must be a
// non-nil pointer to a slice (for example *[]Task). A nil filter matches
// every document. When nothing matches, out is set to an empty, non-nil
// slice. Options such as sorting and paging are passed through to the
// driver:
//
//	err := db.Find(ctx, "tasks", bson.M{"done": false}, &tasks,
//		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(20))
func (m *MongoDB) Find(ctx context.Context, collection string, filter any, out any, opts ...options.Lister[options.FindOptions]) error {
	slice, err := slicePointer(out)
	if err != nil {
		return err
	}

	cursor, err := m.Collection(collection).Find(ctx, readFilter(filter), opts...)
	if err != nil {
		return err
	}
	// All closes the cursor itself, including on failure.
	if err := cursor.All(ctx, out); err != nil {
		return err
	}

	if slice.IsNil() {
		slice.Set(reflect.MakeSlice(slice.Type(), 0, 0))
	}
	return nil
}

// Count returns the number of documents matching filter. A nil filter
// counts every document.
func (m *MongoDB) Count(ctx context.Context, collection string, filter any) (int64, error) {
	return m.Collection(collection).CountDocuments(ctx, readFilter(filter))
}

// UpdateOne applies update (for example bson.M{"$set": ...}) to the
// first document matching filter. When nothing matches it returns an
// error that matches both ErrNotFound and mongo.ErrNoDocuments. A match
// whose update changes nothing is not an error.
func (m *MongoDB) UpdateOne(ctx context.Context, collection string, filter, update any) error {
	res, err := m.Collection(collection).UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return notFound(mongo.ErrNoDocuments)
	}
	return nil
}

// UpdateMany applies update to every document matching filter and
// returns how many documents matched. Matching nothing is not an error.
// The filter is required: pass bson.M{} to update every document.
func (m *MongoDB) UpdateMany(ctx context.Context, collection string, filter, update any) (int64, error) {
	res, err := m.Collection(collection).UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, err
	}
	return res.MatchedCount, nil
}

// DeleteOne deletes the first document matching filter. When nothing was
// deleted it returns an error that matches both ErrNotFound and
// mongo.ErrNoDocuments.
func (m *MongoDB) DeleteOne(ctx context.Context, collection string, filter any) error {
	res, err := m.Collection(collection).DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return notFound(mongo.ErrNoDocuments)
	}
	return nil
}

// DeleteMany deletes every document matching filter and returns how many
// were deleted. Deleting nothing is not an error. The filter is required:
// pass bson.M{} to delete every document.
func (m *MongoDB) DeleteMany(ctx context.Context, collection string, filter any) (int64, error) {
	res, err := m.Collection(collection).DeleteMany(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// readFilter turns a nil filter into an empty document, which matches
// everything. Only read helpers use it: write helpers keep the driver's
// rejection of a nil filter so a forgotten filter cannot silently modify
// or delete a whole collection.
func readFilter(filter any) any {
	if filter == nil {
		return bson.D{}
	}
	return filter
}

// slicePointer checks that out is a non-nil pointer to a slice and
// returns the addressable slice value.
func slicePointer(out any) (reflect.Value, error) {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Slice {
		return reflect.Value{}, fmt.Errorf("database: Find needs a non-nil pointer to a slice, got %T", out)
	}
	return v.Elem(), nil
}
