package database

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"
)

func TestMongoURLDatabase(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"host only", "mongodb://localhost", ""},
		{"host and port", "mongodb://localhost:27017", ""},
		{"trailing slash", "mongodb://localhost:27017/", ""},
		{"database", "mongodb://localhost:27017/myapp", "myapp"},
		{"database and query", "mongodb://localhost:27017/myapp?retryWrites=true&w=majority", "myapp"},
		{"slash then query", "mongodb://localhost:27017/?authSource=admin", ""},
		{"credentials", "mongodb://user:secret@localhost:27017/myapp", "myapp"},
		{"credentials without database", "mongodb://user:secret@localhost:27017", ""},
		{"username only", "mongodb://user@localhost/myapp", "myapp"},
		{"encoded credentials", "mongodb://us%40er:p%2Fa%3Ass@localhost/myapp", "myapp"},
		{"question mark in password", "mongodb://user:p?ss@localhost/myapp", "myapp"},
		{"replica set", "mongodb://a.example.com:27017,b.example.com:27018,c.example.com/myapp?replicaSet=rs0", "myapp"},
		{"ipv6 host", "mongodb://[::1]:27017/myapp", "myapp"},
		{"percent-encoded database", "mongodb://localhost/my%2Dapp", "my-app"},
		{"plus decodes to space like the driver", "mongodb://localhost/my+app", "my app"},
		{"unix socket", "mongodb://%2Ftmp%2Fmongodb-27017.sock/myapp", "myapp"},
		{"query containing slash", "mongodb://localhost/?authMechanismProperties=SERVICE_NAME:a/b", ""},
		{"srv host only", "mongodb+srv://cluster0.example.net", ""},
		{"srv trailing slash", "mongodb+srv://cluster0.example.net/", ""},
		{"srv database", "mongodb+srv://user:secret@cluster0.example.net/myapp", "myapp"},
		{"srv database and query", "mongodb+srv://user:secret@cluster0.example.net/myapp?retryWrites=true&w=majority", "myapp"},
		{"srv query without database", "mongodb+srv://user:secret@cluster0.example.net/?retryWrites=true", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mongoURLDatabase(tt.url)
			if err != nil {
				t.Fatalf("mongoURLDatabase(%q) error: %v", tt.url, err)
			}
			if got != tt.want {
				t.Fatalf("mongoURLDatabase(%q) = %q, want %q", tt.url, got, tt.want)
			}

			// The driver's own parser must agree, so the helpers operate on
			// the same database the driver authenticates against. SRV URLs
			// are skipped because the driver resolves them over DNS.
			if strings.HasPrefix(tt.url, "mongodb+srv://") {
				return
			}
			cs, err := connstring.Parse(tt.url)
			if err != nil {
				t.Fatalf("driver rejected test URL %q: %v", tt.url, err)
			}
			if cs.Database != got {
				t.Fatalf("driver database for %q = %q, ours = %q", tt.url, cs.Database, got)
			}
		})
	}
}

func TestMongoURLDatabaseErrors(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"no scheme", "localhost:27017/myapp"},
		{"wrong scheme", "postgres://localhost/myapp"},
		{"uppercase scheme", "MONGODB://localhost/myapp"},
		{"scheme without slashes", "mongodb:localhost/myapp"},
		{"malformed percent-encoding", "mongodb://localhost/my%zzapp"},
		{"truncated percent-encoding", "mongodb://localhost/myapp%2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := mongoURLDatabase(tt.url); err == nil {
				t.Fatalf("mongoURLDatabase(%q) succeeded, want an error", tt.url)
			}
		})
	}
}

func TestMongoURLDatabaseErrorsNeverLeakTheURL(t *testing.T) {
	urls := []string{
		"mongodbx://user:topsecret@localhost/myapp",
		"mongodb://user:topsecret@localhost/my%zzapp",
	}
	for _, u := range urls {
		_, err := mongoURLDatabase(u)
		if err == nil {
			t.Fatalf("mongoURLDatabase(%q) succeeded, want an error", u)
		}
		if strings.Contains(err.Error(), "topsecret") {
			t.Fatalf("error %q leaks the password", err)
		}
	}
}

func TestResolveMongoDatabase(t *testing.T) {
	tests := []struct {
		name string
		cfg  MongoConfig
		want string
	}{
		{"default", MongoConfig{URL: "mongodb://localhost:27017"}, DefaultMongoDatabase},
		{"default with trailing slash", MongoConfig{URL: "mongodb://localhost:27017/"}, DefaultMongoDatabase},
		{"from url", MongoConfig{URL: "mongodb://localhost:27017/fromurl"}, "fromurl"},
		{"from srv url", MongoConfig{URL: "mongodb+srv://u:p@cluster.example.net/fromurl?w=majority"}, "fromurl"},
		{"explicit wins over url", MongoConfig{URL: "mongodb://localhost/fromurl", Database: "explicit"}, "explicit"},
		{"explicit without url database", MongoConfig{URL: "mongodb://localhost", Database: "explicit"}, "explicit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMongoDatabase(tt.cfg)
			if err != nil {
				t.Fatalf("resolveMongoDatabase(%+v) error: %v", tt.cfg, err)
			}
			if got != tt.want {
				t.Fatalf("resolveMongoDatabase(%+v) = %q, want %q", tt.cfg, got, tt.want)
			}
		})
	}
}

func TestResolveMongoDatabaseErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  MongoConfig
	}{
		{"empty url", MongoConfig{}},
		{"blank url", MongoConfig{URL: "   "}},
		{"empty url with database", MongoConfig{Database: "myapp"}},
		{"invalid scheme", MongoConfig{URL: "http://localhost"}},
		{"invalid url even with explicit database", MongoConfig{URL: "http://localhost", Database: "myapp"}},
		{"invalid explicit database", MongoConfig{URL: "mongodb://localhost", Database: "my.app"}},
		{"invalid database in url", MongoConfig{URL: "mongodb://localhost/my.app"}},
		{"space from plus in url", MongoConfig{URL: "mongodb://localhost/my+app"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := resolveMongoDatabase(tt.cfg); err == nil {
				t.Fatalf("resolveMongoDatabase(%+v) = %q, want an error", tt.cfg, got)
			}
		})
	}
}

func TestValidateMongoDatabaseName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"simple", "myapp", true},
		{"underscore and digits", "my_app_2", true},
		{"dash", "my-app", true},
		{"non-ascii", "naïve_app", true},
		{"max length", strings.Repeat("a", 63), true},
		{"empty", "", false},
		{"too long", strings.Repeat("a", 64), false},
		{"slash", "my/app", false},
		{"backslash", `my\app`, false},
		{"dot", "my.app", false},
		{"space", "my app", false},
		{"quote", `my"app`, false},
		{"dollar", "my$app", false},
		{"nul", "my\x00app", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMongoDatabaseName(tt.input)
			if tt.valid && err != nil {
				t.Fatalf("validateMongoDatabaseName(%q) = %v, want nil", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("validateMongoDatabaseName(%q) = nil, want an error", tt.input)
			}
		})
	}
}

func TestNewMongoDBRejectsEmptyURL(t *testing.T) {
	for _, url := range []string{"", "  \t"} {
		db, err := NewMongoDB(MongoConfig{URL: url, Database: "myapp"})
		if err == nil || db != nil {
			t.Fatalf("NewMongoDB(URL=%q) = (%v, %v), want (nil, error)", url, db, err)
		}
		if !strings.Contains(err.Error(), "URL is empty") {
			t.Fatalf("NewMongoDB(URL=%q) error = %q, want it to mention the empty URL", url, err)
		}
	}
}

func TestNewMongoDBRejectsInvalidURL(t *testing.T) {
	db, err := NewMongoDB(MongoConfig{URL: "http://localhost:27017"})
	if err == nil || db != nil {
		t.Fatalf("NewMongoDB(http URL) = (%v, %v), want (nil, error)", db, err)
	}
}

func TestNewPostgresDBRejectsEmptyURL(t *testing.T) {
	for _, url := range []string{"", "  \t"} {
		db, err := NewPostgresDB(PostgresConfig{URL: url})
		if err == nil || db != nil {
			t.Fatalf("NewPostgresDB(URL=%q) = (%v, %v), want (nil, error)", url, db, err)
		}
		if !strings.Contains(err.Error(), "URL is empty") {
			t.Fatalf("NewPostgresDB(URL=%q) error = %q, want it to mention the empty URL", url, err)
		}
	}
}

func TestNewPostgresDBRejectsMalformedURLWithoutLeakingPassword(t *testing.T) {
	db, err := NewPostgresDB(PostgresConfig{URL: "postgres://user:topsecret@localhost:notaport/db"})
	if err == nil || db != nil {
		t.Fatalf("NewPostgresDB(malformed) = (%v, %v), want (nil, error)", db, err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error %q leaks the password", err)
	}
}
