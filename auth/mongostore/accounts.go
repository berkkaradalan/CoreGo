package mongostore

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/berkkaradalan/CoreGo/auth"
)

// CreateAccount inserts account, filling in ID and timestamps on the
// passed struct when the caller left them zero and setting Version to 1
// (also on the passed struct). A duplicate (provider,
// provider_account_id) or (user_id, provider) pair, or a duplicate ID,
// yields auth.ErrDuplicate.
func (s *Store) CreateAccount(ctx context.Context, account *auth.Account) error {
	account.ID = ensureID(account.ID)
	ensureCreateTimestamps(&account.CreatedAt, &account.UpdatedAt)
	account.Version = 1

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	_, err := s.accounts.InsertOne(ctx, accountToDoc(account))
	return mapError(err)
}

// GetAccount fetches an account by provider and provider specific
// account ID. It returns auth.ErrNotFound when nothing matches.
func (s *Store) GetAccount(ctx context.Context, provider, providerAccountID string) (*auth.Account, error) {
	return s.findAccount(ctx, bson.D{
		{Key: "provider", Value: provider},
		{Key: "provider_account_id", Value: providerAccountID},
	})
}

// GetAccountByUser fetches the account of provider owned by userID. It
// returns auth.ErrNotFound when the user has no such account.
func (s *Store) GetAccountByUser(ctx context.Context, userID, provider string) (*auth.Account, error) {
	return s.findAccount(ctx, bson.D{
		{Key: "user_id", Value: userID},
		{Key: "provider", Value: provider},
	})
}

// findAccount returns the first account matching filter.
func (s *Store) findAccount(ctx context.Context, filter bson.D) (*auth.Account, error) {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	var doc accountDoc
	if err := s.accounts.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, mapError(err)
	}
	return docToAccount(&doc), nil
}

// ListAccountsByUser returns the user's accounts oldest first
// (created_at ascending, the ID ascending as tie-breaker). The result
// is an empty non-nil slice when the user has no accounts.
func (s *Store) ListAccountsByUser(ctx context.Context, userID string) ([]*auth.Account, error) {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	cursor, err := s.accounts.Find(ctx, bson.D{{Key: "user_id", Value: userID}}, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, mapError(err)
	}
	var docs []accountDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, mapError(err)
	}
	accounts := make([]*auth.Account, 0, len(docs))
	for i := range docs {
		accounts = append(accounts, docToAccount(&docs[i]))
	}
	return accounts, nil
}

// UpdateAccount replaces the full account record with optimistic
// concurrency, exactly like UpdateUser: the document is written only
// while its stored version equals account.Version, and on success
// Version and UpdatedAt advance in the document and on the passed
// struct. It returns auth.ErrNotFound when no account has account.ID,
// auth.ErrConflict when the stored version differs and
// auth.ErrDuplicate when the change collides with another account's
// unique pair. The passed struct is only modified on success.
func (s *Store) UpdateAccount(ctx context.Context, account *auth.Account) error {
	doc := accountToDoc(account)
	doc.UpdatedAt = storeNow()
	doc.Version = account.Version + 1

	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	if err := replaceVersioned(ctx, s.accounts, account.ID, account.Version, doc); err != nil {
		return err
	}
	account.UpdatedAt = doc.UpdatedAt
	account.Version = doc.Version
	return nil
}

// DeleteAccount removes an account by ID. It returns auth.ErrNotFound
// when no account has the given ID.
func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()

	result, err := s.accounts.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return mapError(err)
	}
	if result.DeletedCount == 0 {
		return auth.ErrNotFound
	}
	return nil
}
