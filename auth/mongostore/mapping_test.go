package mongostore

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/berkkaradalan/CoreGo/auth"
)

func TestUserMappingRoundTrip(t *testing.T) {
	banExpires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	user := &auth.User{
		ID:            "user-1",
		Name:          "Ada",
		Email:         "ada@example.com",
		EmailVerified: true,
		Image:         "https://example.com/a.png",
		Banned:        true,
		BanReason:     "abuse",
		BanExpires:    &banExpires,
		CreatedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Version:       7,
	}

	doc := userToDoc(user)
	if doc.BanExpires == user.BanExpires {
		t.Fatal("userToDoc shares the BanExpires pointer with the record")
	}
	got := docToUser(doc)
	if got.BanExpires == doc.BanExpires {
		t.Fatal("docToUser shares the BanExpires pointer with the document")
	}
	if !got.BanExpires.Equal(banExpires) {
		t.Fatalf("BanExpires = %v, want %v", got.BanExpires, banExpires)
	}
	got.BanExpires, user.BanExpires = nil, nil
	if *got != *user {
		t.Fatalf("user round trip mismatch:\n got %+v\nwant %+v", *got, *user)
	}

	// A nil ban expiry survives the round trip as nil.
	if got := docToUser(userToDoc(user)); got.BanExpires != nil {
		t.Fatalf("nil BanExpires became %v", *got.BanExpires)
	}
}

func TestAccountMappingRoundTrip(t *testing.T) {
	account := &auth.Account{
		ID:                "account-1",
		UserID:            "user-1",
		Provider:          "credential",
		ProviderAccountID: "ada@example.com",
		Password:          "$2a$10$hash",
		CreatedAt:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Version:           3,
	}
	if got := docToAccount(accountToDoc(account)); *got != *account {
		t.Fatalf("account round trip mismatch:\n got %+v\nwant %+v", *got, *account)
	}
}

func TestSessionMappingRoundTrip(t *testing.T) {
	session := &auth.Session{
		ID:        "session-1",
		UserID:    "user-1",
		TokenHash: "abc123",
		ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		IPAddress: "192.0.2.1",
		UserAgent: "test-agent",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	if got := docToSession(sessionToDoc(session)); *got != *session {
		t.Fatalf("session round trip mismatch:\n got %+v\nwant %+v", *got, *session)
	}
}

func TestTokenMappingRoundTrip(t *testing.T) {
	token := &auth.VerificationToken{
		ID:         "token-1",
		Identifier: "email-verification:ada@example.com",
		ValueHash:  "def456",
		ExpiresAt:  time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if got := docToToken(tokenToDoc(token)); *got != *token {
		t.Fatalf("token round trip mismatch:\n got %+v\nwant %+v", *got, *token)
	}
}

// TestDocumentFieldNames pins the persisted field names: they are part
// of the data format and documented for users who query the
// collections directly.
func TestDocumentFieldNames(t *testing.T) {
	expires := time.Now()
	tests := []struct {
		name string
		doc  any
		want []string
	}{
		{name: "user", doc: userToDoc(&auth.User{BanExpires: &expires}), want: []string{
			"_id", "name", "email", "email_verified", "image", "banned", "ban_reason", "ban_expires", "created_at", "updated_at", "version"}},
		{name: "account", doc: accountToDoc(&auth.Account{}), want: []string{
			"_id", "user_id", "provider", "provider_account_id", "password", "created_at", "updated_at", "version"}},
		{name: "session", doc: sessionToDoc(&auth.Session{}), want: []string{
			"_id", "user_id", "token_hash", "expires_at", "ip_address", "user_agent", "created_at", "updated_at"}},
		{name: "verification token", doc: tokenToDoc(&auth.VerificationToken{}), want: []string{
			"_id", "identifier", "value_hash", "expires_at", "created_at"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := bson.Marshal(tt.doc)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			elems, err := bson.Raw(raw).Elements()
			if err != nil {
				t.Fatalf("Elements: %v", err)
			}
			if len(elems) != len(tt.want) {
				t.Fatalf("document has %d fields, want %d (%v)", len(elems), len(tt.want), tt.want)
			}
			for i, elem := range elems {
				if elem.Key() != tt.want[i] {
					t.Fatalf("field %d = %q, want %q", i, elem.Key(), tt.want[i])
				}
			}
		})
	}
}

// TestBSONTimePrecision documents what the driver does with time.Time
// values: it floors them to milliseconds and decodes them in UTC. The
// store's guarantees about timestamps build on exactly this behavior,
// so a driver change must fail here first.
func TestBSONTimePrecision(t *testing.T) {
	berlin := time.FixedZone("UTC+2", 2*60*60)
	in := time.Date(2026, 10, 23, 12, 30, 45, 123_987_654, berlin)

	raw, err := bson.Marshal(tokenToDoc(&auth.VerificationToken{CreatedAt: in, ExpiresAt: in}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var doc verificationTokenDoc
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	want := time.Date(2026, 10, 23, 10, 30, 45, 123_000_000, time.UTC)
	if !doc.CreatedAt.Equal(want) {
		t.Fatalf("decoded = %v, want %v (floored to the millisecond)", doc.CreatedAt, want)
	}
	if doc.CreatedAt.Location() != time.UTC {
		t.Fatalf("decoded location = %v, want UTC", doc.CreatedAt.Location())
	}
	if doc.CreatedAt != want {
		t.Fatalf("decoded value %#v is not identical to %#v", doc.CreatedAt, want)
	}
}

// TestBSONTimeTruncationPreservesOrder covers the property the auth
// service relies on: a token created at or after its user's creation
// instant never reads back as created before the user, including when
// both fall into the same millisecond.
func TestBSONTimeTruncationPreservesOrder(t *testing.T) {
	base := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		userCreated   time.Time
		tokenCreated  time.Time
		wantSameValue bool
	}{
		{name: "identical instant", userCreated: base.Add(500 * time.Microsecond), tokenCreated: base.Add(500 * time.Microsecond), wantSameValue: true},
		{name: "token later in the same millisecond", userCreated: base.Add(100 * time.Microsecond), tokenCreated: base.Add(900 * time.Microsecond), wantSameValue: true},
		{name: "user at the very end of a millisecond", userCreated: base.Add(time.Millisecond - time.Nanosecond), tokenCreated: base.Add(time.Millisecond - time.Nanosecond), wantSameValue: true},
		{name: "token in the next millisecond", userCreated: base.Add(999 * time.Microsecond), tokenCreated: base.Add(time.Millisecond), wantSameValue: false},
		{name: "whole milliseconds", userCreated: base, tokenCreated: base, wantSameValue: true},
		{name: "before the Unix epoch", userCreated: time.Date(1960, 1, 1, 0, 0, 0, 100_000, time.UTC), tokenCreated: time.Date(1960, 1, 1, 0, 0, 0, 800_000, time.UTC), wantSameValue: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := roundTripTime(t, tt.userCreated)
			token := roundTripTime(t, tt.tokenCreated)
			if token.Before(user) {
				t.Fatalf("token %v reads back before user %v", token, user)
			}
			if got := token.Equal(user); got != tt.wantSameValue {
				t.Fatalf("token == user after round trip: %v, want %v", got, tt.wantSameValue)
			}
		})
	}
}

// roundTripTime encodes t the way the store persists it and decodes it
// again.
func roundTripTime(t *testing.T, in time.Time) time.Time {
	t.Helper()
	raw, err := bson.Marshal(bson.D{{Key: "t", Value: in}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out struct {
		T time.Time `bson:"t"`
	}
	if err := bson.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return out.T
}

func TestStoreNowHasMillisecondPrecision(t *testing.T) {
	before := time.Now().Add(-time.Second)
	now := storeNow()
	if now.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatalf("storeNow() = %v carries sub-millisecond digits", now)
	}
	if now.Location() != time.UTC {
		t.Fatalf("storeNow() location = %v, want UTC", now.Location())
	}
	if now.Before(before) || now.After(time.Now()) {
		t.Fatalf("storeNow() = %v is not close to the current time", now)
	}
	if got := roundTripTime(t, now); got != now {
		t.Fatalf("storeNow() value %#v does not survive a BSON round trip unchanged (%#v)", now, got)
	}
}

func TestNewID(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := newID()
		if len(id) != 32 {
			t.Fatalf("newID() length = %d, want 32 (id = %q)", len(id), id)
		}
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Fatalf("newID() = %q, want lowercase hex characters only", id)
			}
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("newID() returned duplicate id %q after %d calls", id, i+1)
		}
		seen[id] = struct{}{}
	}
}

func TestEnsureID(t *testing.T) {
	if got := ensureID(""); len(got) != 32 {
		t.Fatalf(`ensureID("") = %q, want a generated 32 character id`, got)
	}
	if got := ensureID("custom-id"); got != "custom-id" {
		t.Fatalf(`ensureID("custom-id") = %q, want it preserved`, got)
	}
}

func TestEnsureCreateTimestamps(t *testing.T) {
	t.Run("fills zero timestamps with the same millisecond precise instant", func(t *testing.T) {
		var created, updated time.Time
		before := time.Now().Add(-time.Second)
		ensureCreateTimestamps(&created, &updated)
		if created.Before(before) || created != updated {
			t.Fatalf("created = %v, updated = %v; want both stamped with the same current instant", created, updated)
		}
		if created.Nanosecond()%int(time.Millisecond) != 0 {
			t.Fatalf("created = %v carries sub-millisecond digits", created)
		}
	})

	t.Run("preserves caller supplied timestamps exactly", func(t *testing.T) {
		wantCreated := time.Date(2024, 5, 1, 10, 0, 0, 123_456_789, time.Local)
		wantUpdated := time.Date(2024, 5, 2, 10, 0, 0, 1, time.UTC)
		created, updated := wantCreated, wantUpdated
		ensureCreateTimestamps(&created, &updated)
		if created != wantCreated || updated != wantUpdated {
			t.Fatalf("created = %v, updated = %v; want the caller values untouched", created, updated)
		}
	})

	t.Run("fills only the zero one", func(t *testing.T) {
		created := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
		var updated time.Time
		ensureCreateTimestamps(&created, &updated)
		if !created.Equal(time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)) || updated.IsZero() {
			t.Fatalf("created = %v, updated = %v", created, updated)
		}
	})

	t.Run("nil updatedAt is accepted", func(t *testing.T) {
		var created time.Time
		ensureCreateTimestamps(&created, nil)
		if created.IsZero() {
			t.Fatal("created was not stamped")
		}
	})
}

func TestCeilMillis(t *testing.T) {
	base := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{name: "whole millisecond is unchanged", in: base, want: base},
		{name: "one nanosecond past rounds up", in: base.Add(time.Nanosecond), want: base.Add(time.Millisecond)},
		{name: "mid millisecond rounds up", in: base.Add(500 * time.Microsecond), want: base.Add(time.Millisecond)},
		{name: "last nanosecond rounds up", in: base.Add(time.Millisecond - time.Nanosecond), want: base.Add(time.Millisecond)},
		{name: "before the Unix epoch", in: time.Date(1960, 1, 1, 0, 0, 0, 1, time.UTC), want: time.Date(1960, 1, 1, 0, 0, 0, int(time.Millisecond), time.UTC)},
		{name: "zero time", in: time.Time{}, want: time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ceilMillis(tt.in); !got.Equal(tt.want) {
				t.Fatalf("ceilMillis(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}

	t.Run("monotonic clock reading is ignored", func(t *testing.T) {
		now := time.Now() // carries a monotonic reading
		got := ceilMillis(now)
		if got.Before(now) || got.Sub(now) >= time.Millisecond {
			t.Fatalf("ceilMillis(%v) = %v", now, got)
		}
	})
}

// TestExpiredBeforeMatchesStrictlyEarlierStoredValues proves the filter
// bound against the values the store can hold (whole milliseconds):
// stored < before must hold exactly when stored < ceilMillis(before).
func TestExpiredBeforeMatchesStrictlyEarlierStoredValues(t *testing.T) {
	base := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	stored := []time.Time{base.Add(-time.Millisecond), base, base.Add(time.Millisecond), base.Add(2 * time.Millisecond)}
	befores := []time.Time{
		base,
		base.Add(time.Nanosecond),
		base.Add(500 * time.Microsecond),
		base.Add(time.Millisecond),
		base.Add(time.Millisecond + time.Nanosecond),
	}
	for _, before := range befores {
		bound := ceilMillis(before)
		for _, s := range stored {
			if got, want := s.Before(bound), s.Before(before); got != want {
				t.Fatalf("stored %v, before %v: filter matches = %v, contract says %v", s, before, got, want)
			}
		}
	}

	filter := expiredBefore(base.Add(time.Nanosecond))
	if len(filter) != 1 || filter[0].Key != "expires_at" {
		t.Fatalf("expiredBefore filter = %v, want a single expires_at condition", filter)
	}
	cond, ok := filter[0].Value.(bson.D)
	if !ok || len(cond) != 1 || cond[0].Key != "$lt" {
		t.Fatalf("expiredBefore condition = %v, want {$lt: ...}", filter[0].Value)
	}
	if bound, _ := cond[0].Value.(time.Time); !bound.Equal(base.Add(time.Millisecond)) {
		t.Fatalf("expiredBefore bound = %v, want %v", cond[0].Value, base.Add(time.Millisecond))
	}
}

func TestNormalizeListOptions(t *testing.T) {
	tests := []struct {
		name       string
		in         auth.ListOptions
		wantLimit  int64
		wantOffset int64
	}{
		{name: "zero limit and offset select the default", in: auth.ListOptions{}, wantLimit: 100, wantOffset: 0},
		{name: "negative limit selects the default", in: auth.ListOptions{Limit: -5}, wantLimit: 100, wantOffset: 0},
		{name: "negative offset clamps to zero", in: auth.ListOptions{Limit: 50, Offset: -10}, wantLimit: 50, wantOffset: 0},
		{name: "large limit is capped", in: auth.ListOptions{Limit: 5000, Offset: 20}, wantLimit: 1000, wantOffset: 20},
		{name: "limit at the cap passes", in: auth.ListOptions{Limit: 1000}, wantLimit: 1000, wantOffset: 0},
		{name: "valid values pass through", in: auth.ListOptions{Limit: 25, Offset: 7}, wantLimit: 25, wantOffset: 7},
		{name: "limit of one is honored", in: auth.ListOptions{Limit: 1}, wantLimit: 1, wantOffset: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, offset := normalizeListOptions(tt.in)
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Fatalf("normalizeListOptions(%+v) = (%d, %d), want (%d, %d)",
					tt.in, limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

func TestVersionFilter(t *testing.T) {
	tests := []struct {
		id      string
		version int64
	}{
		{"user-1", 1},
		{"user-2", 0},
		{"", 42},
	}
	for _, tt := range tests {
		got := versionFilter(tt.id, tt.version)
		want := bson.D{{Key: "_id", Value: tt.id}, {Key: "version", Value: tt.version}}
		if len(got) != len(want) {
			t.Fatalf("versionFilter(%q, %d) = %v, want %v", tt.id, tt.version, got, want)
		}
		for i := range want {
			if got[i].Key != want[i].Key || got[i].Value != want[i].Value {
				t.Fatalf("versionFilter(%q, %d) = %v, want %v", tt.id, tt.version, got, want)
			}
		}
	}
}

// TestVersionIsStoredAsInt64 makes sure the version is encoded as a
// BSON int64, the type the update filter compares against.
func TestVersionIsStoredAsInt64(t *testing.T) {
	for name, doc := range map[string]any{
		"user":    userToDoc(&auth.User{Version: 5}),
		"account": accountToDoc(&auth.Account{Version: 5}),
	} {
		raw, err := bson.Marshal(doc)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", name, err)
		}
		value := bson.Raw(raw).Lookup("version")
		if value.Type != bson.TypeInt64 || value.Int64() != 5 {
			t.Fatalf("%s: version = %v (type %v), want int64 5", name, value, value.Type)
		}
	}
}
