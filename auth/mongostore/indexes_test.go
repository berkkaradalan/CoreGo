package mongostore

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestIndexSpecs(t *testing.T) {
	store := &Store{names: newCollectionNames("app_")}
	specs := store.indexSpecs()

	type want struct {
		collection string
		keys       []indexKey
		unique     bool
		ttl        bool
	}
	expected := map[string]want{
		"ux_users_email": {"app_users", []indexKey{{"email", 1}}, true, false},
		"ux_accounts_provider_provider_account_id":     {"app_accounts", []indexKey{{"provider", 1}, {"provider_account_id", 1}}, true, false},
		"ux_accounts_user_id_provider":                 {"app_accounts", []indexKey{{"user_id", 1}, {"provider", 1}}, true, false},
		"ux_sessions_token_hash":                       {"app_sessions", []indexKey{{"token_hash", 1}}, true, false},
		"ix_sessions_user_id_created_at":               {"app_sessions", []indexKey{{"user_id", 1}, {"created_at", -1}}, false, false},
		"ttl_sessions_expires_at":                      {"app_sessions", []indexKey{{"expires_at", 1}}, false, true},
		"ux_verification_tokens_identifier_value_hash": {"app_verification_tokens", []indexKey{{"identifier", 1}, {"value_hash", 1}}, true, false},
		"ttl_verification_tokens_expires_at":           {"app_verification_tokens", []indexKey{{"expires_at", 1}}, false, true},
	}
	if len(specs) != len(expected) {
		t.Fatalf("indexSpecs returned %d indexes, want %d", len(specs), len(expected))
	}

	seenCollections := map[string]bool{}
	previous := ""
	for _, spec := range specs {
		w, ok := expected[spec.name]
		if !ok {
			t.Fatalf("unexpected index %q", spec.name)
		}
		if spec.collection != w.collection || spec.unique != w.unique || spec.ttl != w.ttl {
			t.Fatalf("index %q = %+v, want %+v", spec.name, spec, w)
		}
		if len(spec.keys) != len(w.keys) {
			t.Fatalf("index %q keys = %v, want %v", spec.name, spec.keys, w.keys)
		}
		for i := range w.keys {
			if spec.keys[i] != w.keys[i] {
				t.Fatalf("index %q keys = %v, want %v", spec.name, spec.keys, w.keys)
			}
		}
		// EnsureIndexes batches consecutive specs per collection, so the
		// indexes of one collection must be adjacent.
		if spec.collection != previous {
			if seenCollections[spec.collection] {
				t.Fatalf("indexes of %q are not adjacent", spec.collection)
			}
			seenCollections[spec.collection] = true
			previous = spec.collection
		}
	}
}

func TestIndexSpecModel(t *testing.T) {
	tests := []struct {
		name       string
		spec       indexSpec
		wantUnique bool
		wantTTL    bool
	}{
		{name: "unique", spec: indexSpec{name: "ux", keys: []indexKey{{"email", 1}}, unique: true}, wantUnique: true},
		{name: "ttl", spec: indexSpec{name: "ttl", keys: []indexKey{{"expires_at", 1}}, ttl: true}, wantTTL: true},
		{name: "plain", spec: indexSpec{name: "ix", keys: []indexKey{{"user_id", 1}, {"created_at", -1}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := tt.spec.model()

			keys, ok := model.Keys.(bson.D)
			if !ok || len(keys) != len(tt.spec.keys) {
				t.Fatalf("model keys = %#v, want an ordered bson.D of %d keys", model.Keys, len(tt.spec.keys))
			}
			for i, k := range tt.spec.keys {
				if keys[i].Key != k.field || keys[i].Value != k.order {
					t.Fatalf("model key %d = %v, want %v", i, keys[i], k)
				}
			}

			var opts options.IndexOptions
			for _, apply := range model.Options.List() {
				if err := apply(&opts); err != nil {
					t.Fatalf("apply option: %v", err)
				}
			}
			if opts.Name == nil || *opts.Name != tt.spec.name {
				t.Fatalf("index name = %v, want %q", opts.Name, tt.spec.name)
			}
			if got := opts.Unique != nil && *opts.Unique; got != tt.wantUnique {
				t.Fatalf("unique = %v, want %v", got, tt.wantUnique)
			}
			if tt.wantTTL {
				if opts.ExpireAfterSeconds == nil || *opts.ExpireAfterSeconds != 0 {
					t.Fatalf("expireAfterSeconds = %v, want 0", opts.ExpireAfterSeconds)
				}
			} else if opts.ExpireAfterSeconds != nil {
				t.Fatalf("expireAfterSeconds = %d, want unset", *opts.ExpireAfterSeconds)
			}
		})
	}
}

func TestIndexSatisfies(t *testing.T) {
	keys := func(t *testing.T, d bson.D) bson.Raw {
		t.Helper()
		raw, err := bson.Marshal(d)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return raw
	}
	yes, no := true, false
	zero, hour := int32(0), int32(3600)

	uniqueSpec := indexSpec{name: "ux", keys: []indexKey{{"provider", 1}, {"provider_account_id", 1}}, unique: true}
	ttlSpec := indexSpec{name: "ttl", keys: []indexKey{{"expires_at", 1}}, ttl: true}
	plainSpec := indexSpec{name: "ix", keys: []indexKey{{"user_id", 1}, {"created_at", -1}}}

	tests := []struct {
		name  string
		index func(t *testing.T) mongo.IndexSpecification
		spec  indexSpec
		want  bool
	}{
		{
			name: "unique index with the same keys under another name",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{Name: "custom", KeysDocument: keys(t, bson.D{{Key: "provider", Value: 1}, {Key: "provider_account_id", Value: 1}}), Unique: &yes}
			},
			spec: uniqueSpec, want: true,
		},
		{
			name: "directions stored as doubles and int64",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "provider", Value: 1.0}, {Key: "provider_account_id", Value: int64(1)}}), Unique: &yes}
			},
			spec: uniqueSpec, want: true,
		},
		{
			name: "same keys but not unique",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "provider", Value: 1}, {Key: "provider_account_id", Value: 1}})}
			},
			spec: uniqueSpec, want: false,
		},
		{
			name: "same keys with unique explicitly false",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "provider", Value: 1}, {Key: "provider_account_id", Value: 1}}), Unique: &no}
			},
			spec: uniqueSpec, want: false,
		},
		{
			name: "keys in a different order",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "provider_account_id", Value: 1}, {Key: "provider", Value: 1}}), Unique: &yes}
			},
			spec: uniqueSpec, want: false,
		},
		{
			name: "prefix of the keys only",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "provider", Value: 1}}), Unique: &yes}
			},
			spec: uniqueSpec, want: false,
		},
		{
			name: "extra key",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "provider", Value: 1}, {Key: "provider_account_id", Value: 1}, {Key: "x", Value: 1}}), Unique: &yes}
			},
			spec: uniqueSpec, want: false,
		},
		{
			name: "opposite direction",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: 1}})}
			},
			spec: plainSpec, want: false,
		},
		{
			name: "text index on the same field",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "expires_at", Value: "text"}}), ExpireAfterSeconds: &zero}
			},
			spec: ttlSpec, want: false,
		},
		{
			name: "ttl index with expireAfterSeconds 0",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "expires_at", Value: 1}}), ExpireAfterSeconds: &zero}
			},
			spec: ttlSpec, want: true,
		},
		{
			name: "ttl index with a tuned expireAfterSeconds",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "expires_at", Value: 1}}), ExpireAfterSeconds: &hour}
			},
			spec: ttlSpec, want: true,
		},
		{
			name: "plain index where a ttl index is required",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "expires_at", Value: 1}})}
			},
			spec: ttlSpec, want: false,
		},
		{
			name: "unique index satisfies a plain spec",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: keys(t, bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}), Unique: &yes}
			},
			spec: plainSpec, want: true,
		},
		{
			name: "malformed key document",
			index: func(t *testing.T) mongo.IndexSpecification {
				return mongo.IndexSpecification{KeysDocument: bson.Raw{0x01, 0x02}}
			},
			spec: plainSpec, want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := indexSatisfies(tt.index(t), tt.spec); got != tt.want {
				t.Fatalf("indexSatisfies = %v, want %v", got, tt.want)
			}
		})
	}
}
