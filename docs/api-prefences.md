# API Reference

Complete API reference for CoreGo.

## Core

### corego.New()

Creates a new CoreGo instance.

```go
func New(config *Config) (*Core, error)
```

**Parameters:**
- `config` - CoreGo configuration (optional, can be nil)

**Returns:**
- `*Core` - CoreGo instance
- `error` - Error if initialization fails

**Example:**
```go
core, err := corego.New(&corego.Config{
    Mongo: &database.MongoConfig{
        URL:      "mongodb://localhost:27017",
        Database: "myapp",
    },
    Auth: &auth.Config{
        Secret:       "secret-key",
        TokenExpiry:  60,
        DatabaseName: "users",
    },
})
```

### Core.Close()

Closes all connections.

```go
func (c *Core) Close() error
```

**Example:**
```go
defer core.Close()
```

## Configuration Types

### corego.Config

```go
type Config struct {
    Mongo *database.MongoConfig
    Auth  *auth.Config
}
```

### database.MongoConfig

```go
type MongoConfig struct {
    URL      string  // MongoDB connection URL
    Database string  // Database name (default: "corego")
}
```

### auth.Config

```go
type Config struct {
    Secret       string  // JWT secret key (required)
    TokenExpiry  int     // Token expiry in minutes (default: 60)
    DatabaseName string  // Collection name for users (default: "users")
}
```

## Authentication

### Signup()

Create a new user account.

```go
func (m *Manager) Signup(req SignupRequest) (*User, string, error)
```

**Parameters:**
```go
type SignupRequest struct {
    Email    string         `json:"email"`
    Password string         `json:"password"`
    Custom   map[string]any `json:"custom"`
}
```

**Returns:**
- `*User` - Created user
- `string` - JWT token
- `error` - Error if signup fails

**Example:**
```go
user, token, err := core.Auth.Signup(auth.SignupRequest{
    Email:    "user@example.com",
    Password: "password123",
    Custom: map[string]any{
        "name": "John Doe",
    },
})
```

### Login()

Authenticate a user.

```go
func (m *Manager) Login(req LoginRequest) (*User, string, error)
```

**Parameters:**
```go
type LoginRequest struct {
    Email    string `json:"email"`
    Password string `json:"password"`
}
```

**Returns:**
- `*User` - Authenticated user
- `string` - JWT token
- `error` - Error if login fails

### GetUserByID()

Get user by ID.

```go
func (m *Manager) GetUserByID(id string) (*User, error)
```

### GetUserByEmail()

Get user by email.

```go
func (m *Manager) GetUserByEmail(email string) (*User, error)
```

### GenerateToken()

Generate JWT token for a user.

```go
func (m *Manager) GenerateToken(userID string) (string, error)
```

### VerifyToken()

Verify and parse JWT token.

```go
func (m *Manager) VerifyToken(tokenString string) (jwt.MapClaims, error)
```

### User Type

```go
type User struct {
    ID        string         `json:"id" bson:"_id,omitempty"`
    Email     string         `json:"email" bson:"email"`
    Password  string         `json:"-" bson:"password"`
    Custom    map[string]any `json:"custom,omitempty" bson:"custom,omitempty"`
    CreatedAt time.Time      `json:"created_at" bson:"created_at"`
}
```

## HTTP Handlers (Gin)

### SignupHandler()

```go
func (m *Manager) SignupHandler() gin.HandlerFunc
```

**Endpoint:** `POST /auth/signup`

**Request:**
```json
{
  "email": "user@example.com",
  "password": "password123",
  "custom": {"name": "John"}
}
```

### LoginHandler()

```go
func (m *Manager) LoginHandler() gin.HandlerFunc
```

**Endpoint:** `POST /auth/login`

### GetProfileHandler()

```go
func (m *Manager) GetProfileHandler() gin.HandlerFunc
```

**Endpoint:** `GET /auth/me`
**Auth:** Required

### UpdateProfileHandler()

```go
func (m *Manager) UpdateProfileHandler() gin.HandlerFunc
```

**Endpoint:** `PATCH /auth/me`
**Auth:** Required

### ChangePasswordHandler()

```go
func (m *Manager) ChangePasswordHandler() gin.HandlerFunc
```

**Endpoint:** `POST /auth/change-password`
**Auth:** Required

### DeleteAccountHandler()

```go
func (m *Manager) DeleteAccountHandler() gin.HandlerFunc
```

**Endpoint:** `DELETE /auth/me`
**Auth:** Required

### Middleware()

JWT authentication middleware.

```go
func (m *Manager) Middleware() gin.HandlerFunc
```

**Usage:**
```go
protected := router.Group("/api")
protected.Use(core.Auth.Middleware())
```

## Database (MongoDB)

### InsertOne()

Insert a single document.

```go
func (m *MongoDB) InsertOne(collection string, document any) (string, error)
```

**Returns:** Inserted document ID

### FindOne()

Find a single document.

```go
func (m *MongoDB) FindOne(collection string, filter any, result any) error
```

### Find()

Find multiple documents.

```go
func (m *MongoDB) Find(collection string, filter any) ([]map[string]any, error)
```

### UpdateOne()

Update a single document.

```go
func (m *MongoDB) UpdateOne(collection string, filter any, update any) error
```

### UpdateMany()

Update multiple documents.

```go
func (m *MongoDB) UpdateMany(collection string, filter, update any) error
```

### DeleteOne()

Delete a single document.

```go
func (m *MongoDB) DeleteOne(collection string, filter any) error
```

### DeleteMany()

Delete multiple documents.

```go
func (m *MongoDB) DeleteMany(collection string, filter any) error
```

### Collection()

Get raw MongoDB collection.

```go
func (m *MongoDB) Collection(name string) *mongo.Collection
```

### GetClient()

Get raw MongoDB client.

```go
func (m *MongoDB) GetClient() *mongo.Client
```

### Disconnect()

Disconnect from MongoDB.

```go
func (m *MongoDB) Disconnect() error
```

## Environment

### LoadEnv()

Load environment variables from `.env`.

```go
func LoadEnv() *Env
```

### Env Type

```go
type Env struct {
    MONGODB_CONNECTION_URL *string
    MONGODB_DATABASE       *string
    // Add your own environment variables
}
```

**Usage:**
```go
core.Env.MONGODB_CONNECTION_URL  // Access env vars
```

## Error Handling

Common errors returned by CoreGo:

**Authentication:**
- `"email is required"`
- `"password is required"`
- `"user with this email already exists"`
- `"invalid credentials"`
- `"user not found"`
- `"invalid token"`
- `"token expired"`

**Database:**
- MongoDB driver errors
- Connection errors
- Validation errors

**Example:**
```go
user, token, err := core.Auth.Login(req)
if err != nil {
    switch err.Error() {
    case "invalid credentials":
        // Handle invalid login
    case "user not found":
        // Handle user not found
    default:
        // Handle other errors
    }
}
```
