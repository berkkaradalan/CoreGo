package pgstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/berkkaradalan/CoreGo/auth"
)

// foreignKeyViolation is the SQLSTATE of a foreign key violation.
const foreignKeyViolation = "23503"

// wantForeignKeyViolation fails unless err is an unmapped foreign key
// violation.
func wantForeignKeyViolation(t *testing.T, op string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != foreignKeyViolation {
		t.Fatalf("%s: error = %v, want a foreign key violation (%s)", op, err, foreignKeyViolation)
	}
	if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrDuplicate) {
		t.Fatalf("%s: foreign key violation was mapped to a sentinel: %v", op, err)
	}
}

func TestIntegrationUsers(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	zone := time.FixedZone("plus2", 2*60*60)
	created := time.Date(2026, 3, 4, 5, 6, 7, 123456789, zone)
	banExpires := time.Date(2027, 1, 1, 0, 0, 0, 999, zone)
	user := &auth.User{
		ID:            "user-fixed-id",
		Name:          "Ada Lovelace",
		Email:         "ada@example.com",
		EmailVerified: true,
		Image:         "https://cdn.example.com/ada.png",
		Banned:        true,
		BanReason:     "testing",
		BanExpires:    &banExpires,
		CreatedAt:     created,
		UpdatedAt:     created,
	}
	wantNoErr(t, "CreateUser", store.CreateUser(ctx, user))

	// Caller supplied values are kept, normalized to UTC microseconds.
	wantCreated := time.Date(2026, 3, 4, 3, 6, 7, 123456000, time.UTC)
	if user.ID != "user-fixed-id" || !user.CreatedAt.Equal(wantCreated) || user.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreateUser normalized the struct to %+v", user)
	}

	got, err := store.GetUser(ctx, user.ID)
	wantNoErr(t, "GetUser", err)
	if !reflect.DeepEqual(got, user) {
		t.Fatalf("GetUser = %+v\nwant      %+v", got, user)
	}

	byEmail, err := store.GetUserByEmail(ctx, "ada@example.com")
	wantNoErr(t, "GetUserByEmail", err)
	if !reflect.DeepEqual(byEmail, user) {
		t.Fatalf("GetUserByEmail = %+v, want %+v", byEmail, user)
	}
	// Emails are matched exactly; the service normalizes them.
	_, err = store.GetUserByEmail(ctx, "ADA@example.com")
	wantErrIs(t, "GetUserByEmail(uppercase)", err, auth.ErrNotFound)

	_, err = store.GetUser(ctx, "missing")
	wantErrIs(t, "GetUser(missing)", err, auth.ErrNotFound)
	_, err = store.GetUserByEmail(ctx, "missing@example.com")
	wantErrIs(t, "GetUserByEmail(missing)", err, auth.ErrNotFound)

	// Generated values.
	fresh := &auth.User{Name: "Grace", Email: uniqueEmail(t)}
	before := time.Now()
	wantNoErr(t, "CreateUser(fresh)", store.CreateUser(ctx, fresh))
	if !uuidPattern.MatchString(fresh.ID) {
		t.Fatalf("generated ID %q is not a UUIDv4", fresh.ID)
	}
	if fresh.CreatedAt.IsZero() || !fresh.CreatedAt.Equal(fresh.UpdatedAt) || fresh.CreatedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("generated timestamps: created %v updated %v", fresh.CreatedAt, fresh.UpdatedAt)
	}
	if fresh.BanExpires != nil {
		t.Fatalf("BanExpires = %v, want nil", fresh.BanExpires)
	}
	gotFresh, err := store.GetUser(ctx, fresh.ID)
	wantNoErr(t, "GetUser(fresh)", err)
	if !reflect.DeepEqual(gotFresh, fresh) {
		t.Fatalf("GetUser(fresh) = %+v, want %+v", gotFresh, fresh)
	}

	// Uniqueness.
	err = store.CreateUser(ctx, &auth.User{Name: "Dup", Email: "ada@example.com"})
	wantErrIs(t, "CreateUser(duplicate email)", err, auth.ErrDuplicate)
	err = store.CreateUser(ctx, &auth.User{ID: user.ID, Name: "Dup", Email: uniqueEmail(t)})
	wantErrIs(t, "CreateUser(duplicate id)", err, auth.ErrDuplicate)

	// Update replaces the record and bumps UpdatedAt only.
	user.Name = "Ada King"
	user.Email = "ada.king@example.com"
	user.Banned = false
	user.BanReason = ""
	user.BanExpires = nil
	previousUpdate := user.UpdatedAt
	wantNoErr(t, "UpdateUser", store.UpdateUser(ctx, user))
	if !user.UpdatedAt.After(previousUpdate) {
		t.Fatalf("UpdatedAt not bumped: %v -> %v", previousUpdate, user.UpdatedAt)
	}
	got, err = store.GetUser(ctx, user.ID)
	wantNoErr(t, "GetUser after update", err)
	if !reflect.DeepEqual(got, user) {
		t.Fatalf("GetUser after update = %+v, want %+v", got, user)
	}
	if !got.CreatedAt.Equal(wantCreated) {
		t.Fatalf("UpdateUser changed CreatedAt to %v", got.CreatedAt)
	}
	_, err = store.GetUserByEmail(ctx, "ada@example.com")
	wantErrIs(t, "old email after update", err, auth.ErrNotFound)

	fresh.Email = user.Email
	wantErrIs(t, "UpdateUser(taken email)", store.UpdateUser(ctx, fresh), auth.ErrDuplicate)
	wantErrIs(t, "UpdateUser(missing)", store.UpdateUser(ctx, &auth.User{ID: "missing", Email: uniqueEmail(t)}), auth.ErrNotFound)

	// Delete.
	wantNoErr(t, "DeleteUser", store.DeleteUser(ctx, user.ID))
	_, err = store.GetUser(ctx, user.ID)
	wantErrIs(t, "GetUser after delete", err, auth.ErrNotFound)
	wantErrIs(t, "DeleteUser(again)", store.DeleteUser(ctx, user.ID), auth.ErrNotFound)
}

func TestIntegrationListAndCountUsers(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	users, err := store.ListUsers(ctx, auth.ListOptions{})
	wantNoErr(t, "ListUsers(empty)", err)
	if users == nil || len(users) != 0 {
		t.Fatalf("ListUsers on an empty store = %#v, want an empty non-nil slice", users)
	}
	if n, err := store.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers(empty) = %d, %v", n, err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// u3a and u3b share a timestamp: the ID breaks the tie (descending).
	specs := []struct {
		id      string
		created time.Time
	}{
		{"u1", base},
		{"u2", base.Add(time.Minute)},
		{"u3a", base.Add(2 * time.Minute)},
		{"u3b", base.Add(2 * time.Minute)},
		{"u4", base.Add(3 * time.Minute)},
	}
	for _, s := range specs {
		u := &auth.User{ID: s.id, Name: s.id, Email: s.id + "@example.com", CreatedAt: s.created}
		wantNoErr(t, "CreateUser", store.CreateUser(ctx, u))
	}
	userID := func(u *auth.User) string { return u.ID }

	tests := []struct {
		name string
		opts auth.ListOptions
		want []string
	}{
		{"defaults list everything newest first", auth.ListOptions{}, []string{"u4", "u3b", "u3a", "u2", "u1"}},
		{"limit", auth.ListOptions{Limit: 2}, []string{"u4", "u3b"}},
		{"limit and offset", auth.ListOptions{Limit: 2, Offset: 2}, []string{"u3a", "u2"}},
		{"offset only", auth.ListOptions{Offset: 4}, []string{"u1"}},
		{"offset past the end", auth.ListOptions{Offset: 10}, []string{}},
		{"negative values fall back", auth.ListOptions{Limit: -1, Offset: -5}, []string{"u4", "u3b", "u3a", "u2", "u1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.ListUsers(ctx, tt.opts)
			wantNoErr(t, "ListUsers", err)
			if got == nil {
				t.Fatal("ListUsers returned nil")
			}
			if ids := ids(got, userID); !reflect.DeepEqual(ids, tt.want) {
				t.Fatalf("ListUsers(%+v) = %v, want %v", tt.opts, ids, tt.want)
			}
		})
	}
	if n, err := store.CountUsers(ctx); err != nil || n != int64(len(specs)) {
		t.Fatalf("CountUsers = %d, %v; want %d", n, err, len(specs))
	}

	// The default limit is 100.
	for i := 0; i < 101; i++ {
		mustCreateUser(t, store, uniqueEmail(t))
	}
	all, err := store.ListUsers(ctx, auth.ListOptions{})
	wantNoErr(t, "ListUsers(default limit)", err)
	if len(all) != defaultListLimit {
		t.Fatalf("ListUsers with the default limit returned %d users, want %d", len(all), defaultListLimit)
	}
}

func TestIntegrationAccounts(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	user := mustCreateUser(t, store, uniqueEmail(t))
	other := mustCreateUser(t, store, uniqueEmail(t))

	empty, err := store.ListAccountsByUser(ctx, user.ID)
	wantNoErr(t, "ListAccountsByUser(empty)", err)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("ListAccountsByUser without accounts = %#v, want an empty non-nil slice", empty)
	}

	base := time.Now().Add(-time.Hour)
	credential := &auth.Account{UserID: user.ID, Provider: "credential", ProviderAccountID: user.Email, Password: "hash", CreatedAt: base}
	wantNoErr(t, "CreateAccount(credential)", store.CreateAccount(ctx, credential))
	if !uuidPattern.MatchString(credential.ID) || credential.UpdatedAt.IsZero() {
		t.Fatalf("CreateAccount did not populate fields: %+v", credential)
	}
	google := &auth.Account{UserID: user.ID, Provider: "google", ProviderAccountID: "google-1", CreatedAt: base.Add(time.Minute)}
	wantNoErr(t, "CreateAccount(google)", store.CreateAccount(ctx, google))

	got, err := store.GetAccount(ctx, "google", "google-1")
	wantNoErr(t, "GetAccount", err)
	if !reflect.DeepEqual(got, google) {
		t.Fatalf("GetAccount = %+v, want %+v", got, google)
	}
	got, err = store.GetAccountByUser(ctx, user.ID, "credential")
	wantNoErr(t, "GetAccountByUser", err)
	if !reflect.DeepEqual(got, credential) {
		t.Fatalf("GetAccountByUser = %+v, want %+v", got, credential)
	}
	_, err = store.GetAccount(ctx, "google", "missing")
	wantErrIs(t, "GetAccount(missing)", err, auth.ErrNotFound)
	_, err = store.GetAccountByUser(ctx, other.ID, "google")
	wantErrIs(t, "GetAccountByUser(missing)", err, auth.ErrNotFound)

	list, err := store.ListAccountsByUser(ctx, user.ID)
	wantNoErr(t, "ListAccountsByUser", err)
	if got := ids(list, func(a *auth.Account) string { return a.ID }); !reflect.DeepEqual(got, []string{credential.ID, google.ID}) {
		t.Fatalf("ListAccountsByUser = %v, want oldest first", got)
	}

	// Both unique constraints and the primary key.
	err = store.CreateAccount(ctx, &auth.Account{UserID: other.ID, Provider: "google", ProviderAccountID: "google-1"})
	wantErrIs(t, "CreateAccount(same provider account)", err, auth.ErrDuplicate)
	err = store.CreateAccount(ctx, &auth.Account{UserID: user.ID, Provider: "google", ProviderAccountID: "google-2"})
	wantErrIs(t, "CreateAccount(second google account for user)", err, auth.ErrDuplicate)
	err = store.CreateAccount(ctx, &auth.Account{ID: google.ID, UserID: other.ID, Provider: "github", ProviderAccountID: "gh"})
	wantErrIs(t, "CreateAccount(duplicate id)", err, auth.ErrDuplicate)
	err = store.CreateAccount(ctx, &auth.Account{UserID: "missing-user", Provider: "github", ProviderAccountID: "gh-x"})
	wantForeignKeyViolation(t, "CreateAccount(unknown user)", err)

	// Update.
	credential.ProviderAccountID = "new@example.com"
	credential.Password = "new-hash"
	previousUpdate := credential.UpdatedAt
	wantNoErr(t, "UpdateAccount", store.UpdateAccount(ctx, credential))
	if !credential.UpdatedAt.After(previousUpdate) {
		t.Fatal("UpdateAccount did not bump UpdatedAt")
	}
	got, err = store.GetAccount(ctx, "credential", "new@example.com")
	wantNoErr(t, "GetAccount after update", err)
	if !reflect.DeepEqual(got, credential) {
		t.Fatalf("GetAccount after update = %+v, want %+v", got, credential)
	}
	otherGitHub := &auth.Account{UserID: other.ID, Provider: "github", ProviderAccountID: "gh-1"}
	wantNoErr(t, "CreateAccount(other github)", store.CreateAccount(ctx, otherGitHub))
	otherGitHub.Provider, otherGitHub.ProviderAccountID = "google", "google-1"
	wantErrIs(t, "UpdateAccount(provider collision)", store.UpdateAccount(ctx, otherGitHub), auth.ErrDuplicate)
	wantErrIs(t, "UpdateAccount(missing)", store.UpdateAccount(ctx, &auth.Account{ID: "missing", UserID: user.ID, Provider: "x", ProviderAccountID: "y"}), auth.ErrNotFound)

	// Delete.
	wantNoErr(t, "DeleteAccount", store.DeleteAccount(ctx, google.ID))
	wantErrIs(t, "DeleteAccount(again)", store.DeleteAccount(ctx, google.ID), auth.ErrNotFound)
	_, err = store.GetAccount(ctx, "google", "google-1")
	wantErrIs(t, "GetAccount after delete", err, auth.ErrNotFound)
}

func TestIntegrationSessions(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	user := mustCreateUser(t, store, uniqueEmail(t))
	other := mustCreateUser(t, store, uniqueEmail(t))

	empty, err := store.ListSessionsByUser(ctx, user.ID)
	wantNoErr(t, "ListSessionsByUser(empty)", err)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("ListSessionsByUser without sessions = %#v, want an empty non-nil slice", empty)
	}

	base := time.Now().UTC().Add(-time.Hour)
	first := &auth.Session{
		UserID:    user.ID,
		TokenHash: auth.HashToken("first"),
		ExpiresAt: base.Add(24*time.Hour + 789*time.Nanosecond),
		IPAddress: "203.0.113.7",
		UserAgent: "test-agent/1.0",
		CreatedAt: base,
	}
	wantNoErr(t, "CreateSession(first)", store.CreateSession(ctx, first))
	if first.ExpiresAt.Nanosecond()%1000 != 0 {
		t.Fatalf("ExpiresAt not normalized: %v", first.ExpiresAt)
	}
	second := &auth.Session{UserID: user.ID, TokenHash: auth.HashToken("second"), ExpiresAt: base.Add(48 * time.Hour), CreatedAt: base.Add(time.Minute)}
	wantNoErr(t, "CreateSession(second)", store.CreateSession(ctx, second))

	got, err := store.GetSession(ctx, first.ID)
	wantNoErr(t, "GetSession", err)
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("GetSession = %+v, want %+v", got, first)
	}
	got, err = store.GetSessionByTokenHash(ctx, auth.HashToken("second"))
	wantNoErr(t, "GetSessionByTokenHash", err)
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("GetSessionByTokenHash = %+v, want %+v", got, second)
	}
	_, err = store.GetSession(ctx, "missing")
	wantErrIs(t, "GetSession(missing)", err, auth.ErrNotFound)
	_, err = store.GetSessionByTokenHash(ctx, "missing")
	wantErrIs(t, "GetSessionByTokenHash(missing)", err, auth.ErrNotFound)

	list, err := store.ListSessionsByUser(ctx, user.ID)
	wantNoErr(t, "ListSessionsByUser", err)
	if got := ids(list, func(s *auth.Session) string { return s.ID }); !reflect.DeepEqual(got, []string{second.ID, first.ID}) {
		t.Fatalf("ListSessionsByUser = %v, want newest first", got)
	}

	err = store.CreateSession(ctx, &auth.Session{UserID: other.ID, TokenHash: auth.HashToken("first"), ExpiresAt: base})
	wantErrIs(t, "CreateSession(duplicate token hash)", err, auth.ErrDuplicate)
	err = store.CreateSession(ctx, &auth.Session{ID: first.ID, UserID: other.ID, TokenHash: auth.HashToken("x"), ExpiresAt: base})
	wantErrIs(t, "CreateSession(duplicate id)", err, auth.ErrDuplicate)
	err = store.CreateSession(ctx, &auth.Session{UserID: "missing-user", TokenHash: auth.HashToken("orphan"), ExpiresAt: base})
	wantForeignKeyViolation(t, "CreateSession(unknown user)", err)

	// Update.
	first.ExpiresAt = base.Add(72 * time.Hour)
	first.IPAddress = "198.51.100.1"
	wantNoErr(t, "UpdateSession", store.UpdateSession(ctx, first))
	got, err = store.GetSession(ctx, first.ID)
	wantNoErr(t, "GetSession after update", err)
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("GetSession after update = %+v, want %+v", got, first)
	}
	first.TokenHash = second.TokenHash
	wantErrIs(t, "UpdateSession(token collision)", store.UpdateSession(ctx, first), auth.ErrDuplicate)
	wantErrIs(t, "UpdateSession(missing)", store.UpdateSession(ctx, &auth.Session{ID: "missing", UserID: user.ID, TokenHash: "h"}), auth.ErrNotFound)

	// Single and bulk deletes.
	wantNoErr(t, "DeleteSession", store.DeleteSession(ctx, second.ID))
	wantErrIs(t, "DeleteSession(again)", store.DeleteSession(ctx, second.ID), auth.ErrNotFound)

	otherSession := mustCreateSession(t, store, other.ID, base.Add(time.Hour))
	mustCreateSession(t, store, user.ID, base.Add(time.Hour))
	wantNoErr(t, "DeleteSessionsByUser", store.DeleteSessionsByUser(ctx, user.ID))
	if left, _ := store.ListSessionsByUser(ctx, user.ID); len(left) != 0 {
		t.Fatalf("DeleteSessionsByUser left %d sessions", len(left))
	}
	if _, err := store.GetSession(ctx, otherSession.ID); err != nil {
		t.Fatalf("DeleteSessionsByUser removed another user's session: %v", err)
	}
	wantNoErr(t, "DeleteSessionsByUser(no sessions)", store.DeleteSessionsByUser(ctx, user.ID))
	wantNoErr(t, "DeleteSessionsByUser(unknown user)", store.DeleteSessionsByUser(ctx, "missing"))
}

func TestIntegrationDeleteExpiredSessions(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)
	user := mustCreateUser(t, store, uniqueEmail(t))

	cutoff := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	expired := mustCreateSession(t, store, user.ID, cutoff.Add(-time.Microsecond))
	boundary := mustCreateSession(t, store, user.ID, cutoff)
	valid := mustCreateSession(t, store, user.ID, cutoff.Add(time.Hour))

	wantNoErr(t, "DeleteExpiredSessions(nothing expired)", store.DeleteExpiredSessions(ctx, cutoff.Add(-time.Hour)))
	if n := countRows(t, store, "corego_sessions", ""); n != 3 {
		t.Fatalf("%d sessions left, want 3", n)
	}

	wantNoErr(t, "DeleteExpiredSessions", store.DeleteExpiredSessions(ctx, cutoff))
	_, err := store.GetSession(ctx, expired.ID)
	wantErrIs(t, "expired session", err, auth.ErrNotFound)
	// "Expired" is strictly before: a session expiring exactly at the
	// cutoff stays.
	for _, s := range []*auth.Session{boundary, valid} {
		if _, err := store.GetSession(ctx, s.ID); err != nil {
			t.Fatalf("session expiring at %v was removed: %v", s.ExpiresAt, err)
		}
	}

	// A cutoff with sub-microsecond digits still removes the session
	// stored at the whole microsecond just before it.
	wantNoErr(t, "DeleteExpiredSessions(nanosecond cutoff)", store.DeleteExpiredSessions(ctx, cutoff.Add(1)))
	_, err = store.GetSession(ctx, boundary.ID)
	wantErrIs(t, "boundary session after a later cutoff", err, auth.ErrNotFound)
	if _, err := store.GetSession(ctx, valid.ID); err != nil {
		t.Fatalf("valid session removed: %v", err)
	}
}

func TestIntegrationVerificationTokens(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	expires := time.Now().UTC().Add(time.Hour)
	created := time.Now().Add(-time.Minute)
	token := &auth.VerificationToken{
		Identifier: "email-verification:ada@example.com",
		ValueHash:  auth.HashToken("value"),
		ExpiresAt:  expires,
		CreatedAt:  created,
	}
	wantNoErr(t, "CreateVerificationToken", store.CreateVerificationToken(ctx, token))
	if !uuidPattern.MatchString(token.ID) || !token.CreatedAt.Equal(normalizeTime(created)) {
		t.Fatalf("CreateVerificationToken did not keep the given CreatedAt: %+v", token)
	}

	got, err := store.GetVerificationToken(ctx, token.Identifier, token.ValueHash)
	wantNoErr(t, "GetVerificationToken", err)
	if !reflect.DeepEqual(got, token) {
		t.Fatalf("GetVerificationToken = %+v, want %+v", got, token)
	}

	// The identifier is matched exactly; the purpose prefix matters.
	_, err = store.GetVerificationToken(ctx, "password-reset:ada@example.com", token.ValueHash)
	wantErrIs(t, "GetVerificationToken(other purpose)", err, auth.ErrNotFound)
	_, err = store.GetVerificationToken(ctx, "EMAIL-VERIFICATION:ada@example.com", token.ValueHash)
	wantErrIs(t, "GetVerificationToken(different case)", err, auth.ErrNotFound)

	// Zero CreatedAt is populated.
	generated := &auth.VerificationToken{Identifier: "password-reset:ada@example.com", ValueHash: token.ValueHash, ExpiresAt: expires}
	wantNoErr(t, "CreateVerificationToken(same hash, other identifier)", store.CreateVerificationToken(ctx, generated))
	if generated.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not populated")
	}

	err = store.CreateVerificationToken(ctx, &auth.VerificationToken{Identifier: token.Identifier, ValueHash: token.ValueHash, ExpiresAt: expires})
	wantErrIs(t, "CreateVerificationToken(duplicate pair)", err, auth.ErrDuplicate)
	err = store.CreateVerificationToken(ctx, &auth.VerificationToken{ID: token.ID, Identifier: "x", ValueHash: "y", ExpiresAt: expires})
	wantErrIs(t, "CreateVerificationToken(duplicate id)", err, auth.ErrDuplicate)

	wantNoErr(t, "DeleteVerificationToken", store.DeleteVerificationToken(ctx, token.ID))
	wantErrIs(t, "DeleteVerificationToken(again)", store.DeleteVerificationToken(ctx, token.ID), auth.ErrNotFound)
	_, err = store.GetVerificationToken(ctx, token.Identifier, token.ValueHash)
	wantErrIs(t, "GetVerificationToken after delete", err, auth.ErrNotFound)
}

func TestIntegrationDeleteVerificationTokensByIdentifier(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)
	expires := time.Now().Add(time.Hour)

	target := "password-reset:ada@example.com"
	mustCreateToken(t, store, target, expires)
	mustCreateToken(t, store, target, expires)
	// Near misses must survive: other purpose, longer address, other
	// case, and LIKE metacharacters that would match as a pattern.
	survivors := []string{
		"email-verification:ada@example.com",
		"password-reset:ada@example.com.evil",
		"password-reset:ADA@example.com",
		"password-reset:ada@example_com",
		"password-reset:%",
	}
	for _, identifier := range survivors {
		mustCreateToken(t, store, identifier, expires)
	}

	wantNoErr(t, "DeleteVerificationTokensByIdentifier", store.DeleteVerificationTokensByIdentifier(ctx, target))
	if n := countRows(t, store, "corego_verification_tokens", "identifier = $1", target); n != 0 {
		t.Fatalf("%d tokens left for %s", n, target)
	}
	for _, identifier := range survivors {
		if n := countRows(t, store, "corego_verification_tokens", "identifier = $1", identifier); n != 1 {
			t.Fatalf("token for %q was removed", identifier)
		}
	}

	// Zero matches is not an error; a pattern is not interpreted.
	wantNoErr(t, "DeleteVerificationTokensByIdentifier(no match)", store.DeleteVerificationTokensByIdentifier(ctx, target))
	wantNoErr(t, "DeleteVerificationTokensByIdentifier(pattern)", store.DeleteVerificationTokensByIdentifier(ctx, "%"))
	if n := countRows(t, store, "corego_verification_tokens", ""); n != len(survivors) {
		t.Fatalf("%d tokens left, want %d", n, len(survivors))
	}
}

func TestIntegrationDeleteExpiredVerificationTokens(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	cutoff := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	expired := mustCreateToken(t, store, "email-verification:a@example.com", cutoff.Add(-time.Microsecond))
	boundary := mustCreateToken(t, store, "email-verification:b@example.com", cutoff)
	valid := mustCreateToken(t, store, "email-verification:c@example.com", cutoff.Add(time.Hour))

	wantNoErr(t, "DeleteExpiredVerificationTokens(nothing expired)", store.DeleteExpiredVerificationTokens(ctx, cutoff.Add(-time.Hour)))
	if n := countRows(t, store, "corego_verification_tokens", ""); n != 3 {
		t.Fatalf("%d tokens left, want 3", n)
	}

	wantNoErr(t, "DeleteExpiredVerificationTokens", store.DeleteExpiredVerificationTokens(ctx, cutoff))
	_, err := store.GetVerificationToken(ctx, expired.Identifier, expired.ValueHash)
	wantErrIs(t, "expired token", err, auth.ErrNotFound)
	for _, tok := range []*auth.VerificationToken{boundary, valid} {
		if _, err := store.GetVerificationToken(ctx, tok.Identifier, tok.ValueHash); err != nil {
			t.Fatalf("token expiring at %v was removed: %v", tok.ExpiresAt, err)
		}
	}

	wantNoErr(t, "DeleteExpiredVerificationTokens(nanosecond cutoff)", store.DeleteExpiredVerificationTokens(ctx, cutoff.Add(1)))
	_, err = store.GetVerificationToken(ctx, boundary.Identifier, boundary.ValueHash)
	wantErrIs(t, "boundary token after a later cutoff", err, auth.ErrNotFound)
	if _, err := store.GetVerificationToken(ctx, valid.Identifier, valid.ValueHash); err != nil {
		t.Fatalf("valid token removed: %v", err)
	}
}

func TestIntegrationDeleteUserCascades(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)
	expires := time.Now().Add(time.Hour)

	user := mustCreateUser(t, store, uniqueEmail(t))
	other := mustCreateUser(t, store, uniqueEmail(t))
	for _, provider := range []string{"credential", "github"} {
		wantNoErr(t, "CreateAccount", store.CreateAccount(ctx, &auth.Account{UserID: user.ID, Provider: provider, ProviderAccountID: provider + "-" + user.ID}))
	}
	otherAccount := &auth.Account{UserID: other.ID, Provider: "credential", ProviderAccountID: other.Email}
	wantNoErr(t, "CreateAccount(other)", store.CreateAccount(ctx, otherAccount))
	mustCreateSession(t, store, user.ID, expires)
	mustCreateSession(t, store, user.ID, expires)
	otherSession := mustCreateSession(t, store, other.ID, expires)
	token := mustCreateToken(t, store, "email-verification:"+user.Email, expires)

	wantNoErr(t, "DeleteUser", store.DeleteUser(ctx, user.ID))

	if n := countRows(t, store, "corego_accounts", "user_id = $1", user.ID); n != 0 {
		t.Fatalf("%d accounts survived the user", n)
	}
	if n := countRows(t, store, "corego_sessions", "user_id = $1", user.ID); n != 0 {
		t.Fatalf("%d sessions survived the user", n)
	}
	// Tokens are the service's responsibility and stay.
	if _, err := store.GetVerificationToken(ctx, token.Identifier, token.ValueHash); err != nil {
		t.Fatalf("DeleteUser removed a verification token: %v", err)
	}
	// Nothing of the other user is touched.
	if _, err := store.GetAccount(ctx, "credential", other.Email); err != nil {
		t.Fatalf("other user's account removed: %v", err)
	}
	if _, err := store.GetSession(ctx, otherSession.ID); err != nil {
		t.Fatalf("other user's session removed: %v", err)
	}
}

// TestIntegrationTimestampsRoundTrip covers the comparison the service
// makes when a token is redeemed: it rejects tokens whose CreatedAt is
// before the user's CreatedAt. A token issued in the same microsecond
// as the sign-up (but some nanoseconds later) must still be accepted
// after both values went through PostgreSQL.
func TestIntegrationTimestampsRoundTrip(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := testContext(t)

	zone := time.FixedZone("minus7", -7*60*60)
	signup := time.Date(2026, 10, 23, 9, 30, 0, 123456100, zone)
	tests := []struct {
		name       string
		tokenDelta time.Duration
		wantBefore bool
	}{
		{"same instant", 0, false},
		{"same microsecond, later nanoseconds", 899, false},
		{"next microsecond", 900, false},
		{"one microsecond earlier", -time.Microsecond, true},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := uniqueEmail(t)
			user := &auth.User{Name: "Same Microsecond", Email: email, CreatedAt: signup, UpdatedAt: signup}
			wantNoErr(t, "CreateUser", store.CreateUser(ctx, user))
			token := &auth.VerificationToken{
				Identifier: "email-verification:" + email,
				ValueHash:  auth.HashToken(email + string(rune('a'+i))),
				ExpiresAt:  signup.Add(time.Hour),
				CreatedAt:  signup.Add(tt.tokenDelta),
			}
			wantNoErr(t, "CreateVerificationToken", store.CreateVerificationToken(ctx, token))

			storedUser, err := store.GetUserByEmail(ctx, email)
			wantNoErr(t, "GetUserByEmail", err)
			storedToken, err := store.GetVerificationToken(ctx, token.Identifier, token.ValueHash)
			wantNoErr(t, "GetVerificationToken", err)

			if got := storedToken.CreatedAt.Before(storedUser.CreatedAt); got != tt.wantBefore {
				t.Fatalf("token.CreatedAt (%v) before user.CreatedAt (%v) = %v, want %v",
					storedToken.CreatedAt, storedUser.CreatedAt, got, tt.wantBefore)
			}
			// The values returned to the caller equal the stored ones.
			if !storedUser.CreatedAt.Equal(user.CreatedAt) || !storedToken.CreatedAt.Equal(token.CreatedAt) {
				t.Fatalf("passed structs (%v, %v) differ from stored values (%v, %v)",
					user.CreatedAt, token.CreatedAt, storedUser.CreatedAt, storedToken.CreatedAt)
			}
			for _, ts := range []time.Time{storedUser.CreatedAt, storedUser.UpdatedAt, storedToken.CreatedAt, storedToken.ExpiresAt} {
				if ts.Location() != time.UTC {
					t.Fatalf("timestamp %v is not in UTC", ts)
				}
			}
		})
	}
}

func TestIntegrationContextCancellation(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.GetUser(ctx, "id")
	wantErrIs(t, "GetUser(canceled)", err, context.Canceled)
	_, err = store.ListUsers(ctx, auth.ListOptions{})
	wantErrIs(t, "ListUsers(canceled)", err, context.Canceled)
	wantErrIs(t, "CreateUser(canceled)", store.CreateUser(ctx, &auth.User{Name: "x", Email: uniqueEmail(t)}), context.Canceled)
	wantErrIs(t, "DeleteUser(canceled)", store.DeleteUser(ctx, "id"), context.Canceled)
	wantErrIs(t, "DeleteExpiredSessions(canceled)", store.DeleteExpiredSessions(ctx, time.Now()), context.Canceled)
	wantErrIs(t, "Migrate(canceled)", store.Migrate(ctx), context.Canceled)

	if n, err := store.CountUsers(testContext(t)); err != nil || n != 0 {
		t.Fatalf("CountUsers after canceled calls = %d, %v; want 0", n, err)
	}
}
