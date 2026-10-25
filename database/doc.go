// Package database connects CoreGo applications to MongoDB and
// PostgreSQL and offers small, context-aware helpers on top of the
// official drivers (go.mongodb.org/mongo-driver/v2 and
// github.com/jackc/pgx/v5).
//
// Both wrappers ping the server on construction and fail fast when it is
// unreachable, so a returned handle is known to be usable:
//
//	mongo, err := database.NewMongoDB(database.MongoConfig{URL: "mongodb://localhost:27017/myapp"})
//	pg, err := database.NewPostgresDB(database.PostgresConfig{URL: "postgres://user:pass@localhost:5432/myapp"})
//
// MongoDB helpers decode straight into your own types:
//
//	var tasks []Task
//	err := mongo.Find(ctx, "tasks", bson.M{"done": false}, &tasks)
//
// PostgreSQL rows are scanned into structs with the generic QueryAll and
// QueryOne functions, which accept anything that satisfies Querier (a
// *PostgresDB, a *pgxpool.Pool, a pgx.Tx or a *pgx.Conn):
//
//	users, err := database.QueryAll[User](ctx, pg, "SELECT id, name FROM users")
//
// Lookups that find nothing return an error that matches ErrNotFound with
// errors.Is (and also the driver's own sentinel, mongo.ErrNoDocuments or
// pgx.ErrNoRows). Every helper runs with the caller's context: there is no
// hidden per-operation timeout. For anything the helpers do not cover,
// Client, Database, Collection and Pool return the raw driver handles.
package database
