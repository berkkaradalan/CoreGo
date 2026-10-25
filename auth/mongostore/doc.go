// Package mongostore implements the auth.Store interface on MongoDB
// (driver go.mongodb.org/mongo-driver/v2), on top of the CoreGo
// database.MongoDB wrapper.
//
// # Setup
//
//	db, err := database.NewMongoDB(database.MongoConfig{URL: "mongodb://localhost:27017/myapp"})
//	if err != nil { ... }
//	store, err := mongostore.NewStore(db, mongostore.Options{})
//	if err != nil { ... }
//	if err := store.EnsureIndexes(ctx); err != nil { ... }
//	svc, err := auth.New(auth.Config{}, store)
//
// The corego facade does all of this automatically when MongoDB is
// configured and selected as the auth store.
//
// # Layout
//
// Every entity lives in its own collection inside the database the
// wrapper is bound to. Collection names carry a configurable prefix
// (Options.CollectionPrefix, default "corego_") so they never collide
// with the application's own collections:
//
//	corego_users                auth.User records
//	corego_accounts             auth.Account records
//	corego_sessions             auth.Session records
//	corego_verification_tokens  auth.VerificationToken records
//
// Entity IDs are portable strings (32 hex characters generated with
// crypto/rand when the caller leaves the ID empty) stored as the
// document _id. ObjectIDs are never used, so IDs stay meaningful
// outside MongoDB and identical in shape across store backends.
// Document fields are snake_case (email_verified, expires_at, ...);
// timestamps are BSON dates.
//
// # Behavior
//
//   - Every method derives a 5 second timeout from the context it
//     receives, so a caller's shorter deadline or cancellation still
//     wins. EnsureIndexes is the exception: it only uses the caller's
//     context.
//   - Driver errors are translated to the auth sentinel errors: a
//     missing document becomes auth.ErrNotFound and a duplicate key
//     violation becomes auth.ErrDuplicate.
//   - Create methods fill in ID, CreatedAt and UpdatedAt when the caller
//     left them zero and keep caller supplied values; update methods
//     replace the whole document and set UpdatedAt to the current time.
//   - Users and accounts carry an optimistic concurrency counter (the
//     version field, auth.User.Version and auth.Account.Version).
//     Create sets it to 1; UpdateUser and UpdateAccount write only when
//     the stored version equals the passed one and then increment it,
//     returning auth.ErrConflict on a mismatch and auth.ErrNotFound for
//     a missing document.
//   - Timestamps are stored with millisecond precision (BSON dates) and
//     read back in UTC. Timestamps the store generates are already
//     millisecond precise, so the values written into the caller's
//     struct match later reads exactly. Truncation preserves order, so
//     the service's "token created before its user" check never rejects
//     a token created in the same millisecond as the user.
//   - List methods return an empty, non-nil slice when nothing matches,
//     with a deterministic order (created_at, then _id).
//   - DeleteUser cascades to the user's accounts and sessions without a
//     transaction (transactions need a replica set); the user document
//     is deleted last so a failed cascade can be retried. Verification
//     tokens are removed by the auth service, not by the store.
//
// # Indexes
//
// Call EnsureIndexes once at startup. It creates the unique indexes the
// duplicate detection relies on and TTL indexes (expireAfterSeconds 0)
// on sessions.expires_at and verification_tokens.expires_at, so MongoDB
// removes expired records in the background. See EnsureIndexes for the
// full list.
package mongostore
