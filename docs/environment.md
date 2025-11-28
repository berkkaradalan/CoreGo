# Environment Variables

CoreGo automatically loads environment variables from `.env` files and provides type-safe access.

## Setup

Create a `.env` file in your project root:

```env
# Database
MONGODB_CONNECTION_URL=mongodb://localhost:27017
MONGODB_DATABASE=myapp

# Authentication
AUTH_SECRET=your-super-secret-jwt-key
AUTH_DATABASE=users

# Server
PORT=8080
HOST=localhost

# Custom variables
API_KEY=your-api-key
DEBUG=true
```

## Accessing Environment Variables

```go
core, _ := corego.New(&corego.Config{})

// Access built-in env vars
if core.Env.MONGODB_CONNECTION_URL != nil {
    fmt.Println("MongoDB URL:", *core.Env.MONGODB_CONNECTION_URL)
}

if core.Env.MONGODB_DATABASE != nil {
    fmt.Println("Database:", *core.Env.MONGODB_DATABASE)
}
```

## Auto-Configuration

CoreGo uses environment variables for automatic configuration:

### MongoDB Auto-Connect

If `MONGODB_CONNECTION_URL` is set, CoreGo automatically connects to MongoDB:

```go
// .env
MONGODB_CONNECTION_URL=mongodb://localhost:27017
MONGODB_DATABASE=myapp
```

```go
// No Mongo config needed!
core, err := corego.New(&corego.Config{
    Auth: &auth.Config{
        Secret: os.Getenv("AUTH_SECRET"),
    },
})

// core.Mongo is ready to use
```

### Manual Override

You can override environment variables with explicit config:

```go
core, err := corego.New(&corego.Config{
    Mongo: &database.MongoConfig{
        URL:      "mongodb://custom-host:27017",  // Overrides .env
        Database: "custom_db",
    },
})
```

## Adding Custom Variables

### 1. Update env.go

Edit `env/env.go` to add your custom variables:

```go
type Env struct {
    MONGODB_CONNECTION_URL *string
    MONGODB_DATABASE       *string

    // Add your custom variables
    API_KEY     *string
    DEBUG       *string
    STRIPE_KEY  *string
    REDIS_URL   *string
}

func LoadEnv() *Env {
    godotenv.Load()

    return &Env{
        MONGODB_CONNECTION_URL: getEnvString("MONGODB_CONNECTION_URL"),
        MONGODB_DATABASE:       getEnvString("MONGODB_DATABASE"),

        // Add your custom variables
        API_KEY:    getEnvString("API_KEY"),
        DEBUG:      getEnvString("DEBUG"),
        STRIPE_KEY: getEnvString("STRIPE_KEY"),
        REDIS_URL:  getEnvString("REDIS_URL"),
    }
}
```

### 2. Access Custom Variables

```go
// In your application
if core.Env.API_KEY != nil {
    apiKey := *core.Env.API_KEY
    // Use the API key
}

if core.Env.DEBUG != nil && *core.Env.DEBUG == "true" {
    // Enable debug mode
}
```

## Helper Functions

### getEnvString()

Get string environment variable:

```go
func getEnvString(key string) *string {
    value := os.Getenv(key)
    if value == "" {
        return nil
    }
    return &value
}
```

### Custom Helpers

Add your own helper functions in `env/env.go`:

```go
// Get env var with default
func getEnvWithDefault(key, defaultValue string) string {
    value := os.Getenv(key)
    if value == "" {
        return defaultValue
    }
    return value
}

// Get boolean env var
func getEnvBool(key string) bool {
    value := os.Getenv(key)
    return value == "true" || value == "1"
}

// Get int env var
func getEnvInt(key string, defaultValue int) int {
    value := os.Getenv(key)
    if value == "" {
        return defaultValue
    }
    intVal, err := strconv.Atoi(value)
    if err != nil {
        return defaultValue
    }
    return intVal
}
```

## Best Practices

### 1. Never Commit .env

Add to `.gitignore`:

```gitignore
.env
.env.local
.env.*.local
```

### 2. Provide .env.example

Create `.env.example` with dummy values:

```env
# Database
MONGODB_CONNECTION_URL=mongodb://localhost:27017
MONGODB_DATABASE=myapp

# Authentication
AUTH_SECRET=change-me-in-production
AUTH_DATABASE=users

# Server
PORT=8080
```

### 3. Validate Required Variables

```go
func validateEnv(env *env.Env) error {
    if env.MONGODB_CONNECTION_URL == nil {
        return errors.New("MONGODB_CONNECTION_URL is required")
    }
    if env.AUTH_SECRET == nil {
        return errors.New("AUTH_SECRET is required")
    }
    return nil
}

func main() {
    core, _ := corego.New(&corego.Config{})

    if err := validateEnv(core.Env); err != nil {
        log.Fatal(err)
    }
}
```

### 4. Use Strong Secrets in Production

```env
# Development
AUTH_SECRET=dev-secret

# Production
AUTH_SECRET=lK9$mP2#qR8@vN5^xT4&wZ7!bC3*dF6
```

## Environment-Specific Files

### Development

`.env.development`:
```env
MONGODB_CONNECTION_URL=mongodb://localhost:27017
DEBUG=true
LOG_LEVEL=debug
```

### Production

`.env.production`:
```env
MONGODB_CONNECTION_URL=mongodb://prod-cluster:27017
DEBUG=false
LOG_LEVEL=error
```

### Loading Specific Environments

```go
func LoadEnv(environment string) *Env {
    // Load base .env
    godotenv.Load()

    // Load environment-specific
    godotenv.Load(fmt.Sprintf(".env.%s", environment))

    return &Env{
        // ...
    }
}
```

## Complete Example

**.env:**
```env
MONGODB_CONNECTION_URL=mongodb://localhost:27017
MONGODB_DATABASE=myapp
AUTH_SECRET=super-secret-key
PORT=8080
DEBUG=true
API_KEY=abc123
```

**main.go:**
```go
package main

import (
    "fmt"
    "log"
    "os"
    "github.com/berkkaradalan/CoreGo"
    "github.com/berkkaradalan/CoreGo/auth"
)

func main() {
    // Initialize CoreGo (auto-loads .env)
    core, err := corego.New(&corego.Config{
        Auth: &auth.Config{
            Secret: getEnv("AUTH_SECRET", "fallback-secret"),
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    defer core.Close()

    // Access environment variables
    port := getEnv("PORT", "8080")
    debug := getEnv("DEBUG", "false")

    if core.Env.API_KEY != nil {
        fmt.Println("API Key configured")
    }

    fmt.Printf("Starting server on port %s (debug: %s)\n", port, debug)
}

func getEnv(key, fallback string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return fallback
}
```
