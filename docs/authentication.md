# Authentication Guide

Complete guide to CoreGo's authentication system.

## Configuration

```go
auth.Config{
    Secret:       "your-jwt-secret-key",  // Required: JWT signing key
    TokenExpiry:  60,                      // Optional: Token expiry in minutes (default: 60)
    DatabaseName: "users",                 // Optional: Collection/table name (default: "users")
}
```

## User Signup

### Programmatic Usage

```go
user, token, err := core.Auth.Signup(auth.SignupRequest{
    Email:    "user@example.com",
    Password: "securePassword123",
    Custom: map[string]any{
        "name": "John Doe",
        "age": 30,
        "role": "developer",
    },
})

if err != nil {
    // Handle error
}

// user.ID - User's unique ID
// token - JWT token for authentication
```

### HTTP Handler (Gin)

```go
router.POST("/auth/signup", core.Auth.SignupHandler())
```

**Request:**
```json
{
  "email": "user@example.com",
  "password": "securePassword123",
  "custom": {
    "name": "John Doe",
    "age": 30
  }
}
```

**Response:**
```json
{
  "user": {
    "id": "507f1f77bcf86cd799439011",
    "email": "user@example.com",
    "custom": {
      "name": "John Doe",
      "age": 30
    }
  },
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

## User Login

### Programmatic Usage

```go
user, token, err := core.Auth.Login(auth.LoginRequest{
    Email:    "user@example.com",
    Password: "securePassword123",
})

if err != nil {
    // Handle error (invalid credentials)
}
```

### HTTP Handler (Gin)

```go
router.POST("/auth/login", core.Auth.LoginHandler())
```

**Request:**
```json
{
  "email": "user@example.com",
  "password": "securePassword123"
}
```

## Protected Routes

### Middleware Usage

```go
// Protect all routes in a group
protected := router.Group("/api")
protected.Use(core.Auth.Middleware())
{
    protected.GET("/profile", handleProfile)
    protected.POST("/data", handleData)
}
```

### Accessing User Info

```go
func handleProfile(c *gin.Context) {
    // Get authenticated user ID from context
    userID, exists := c.Get("userID")
    if !exists {
        c.JSON(401, gin.H{"error": "unauthorized"})
        return
    }

    // Use the user ID
    user, err := core.Auth.GetUserByID(userID.(string))
    // ...
}
```

## User Management

### Get User by ID

```go
user, err := core.Auth.GetUserByID("507f1f77bcf86cd799439011")
```

### Get User by Email

```go
user, err := core.Auth.GetUserByEmail("user@example.com")
```

### Update Profile

**Handler:**
```go
router.PATCH("/profile", core.Auth.Middleware(), core.Auth.UpdateProfileHandler())
```

**Request:**
```json
{
  "custom": {
    "name": "Jane Doe",
    "bio": "Full Stack Developer",
    "location": "San Francisco"
  }
}
```

### Change Password

**Handler:**
```go
router.POST("/change-password", core.Auth.Middleware(), core.Auth.ChangePasswordHandler())
```

**Request:**
```json
{
  "old_password": "oldPassword123",
  "new_password": "newSecurePassword456"
}
```

### Delete Account

**Handler:**
```go
router.DELETE("/account", core.Auth.Middleware(), core.Auth.DeleteAccountHandler())
```

## Token Management

### Generate Token

```go
token, err := core.Auth.GenerateToken(userID)
```

### Verify Token

```go
claims, err := core.Auth.VerifyToken(tokenString)
if err != nil {
    // Invalid or expired token
}

userID := claims["user_id"].(string)
```

## Custom User Data

The `custom` field allows you to store any additional user data:

```go
custom := map[string]any{
    "name": "John Doe",
    "avatar": "https://example.com/avatar.jpg",
    "preferences": map[string]any{
        "theme": "dark",
        "language": "en",
    },
    "metadata": []string{"tag1", "tag2"},
}
```

## Security Best Practices

1. **Strong Secrets**: Use long, random strings for JWT secrets
   ```go
   Secret: os.Getenv("AUTH_SECRET") // Load from environment
   ```

2. **Password Requirements**: Implement password validation
   ```go
   if len(password) < 8 {
       return errors.New("password must be at least 8 characters")
   }
   ```

3. **Token Expiry**: Set appropriate expiry times
   ```go
   TokenExpiry: 60 // 1 hour for production
   ```

4. **HTTPS Only**: Always use HTTPS in production

5. **Rate Limiting**: Implement rate limiting on auth endpoints

## Error Handling

```go
user, token, err := core.Auth.Signup(req)
if err != nil {
    switch err.Error() {
    case "email is required":
        // Handle validation error
    case "user with this email already exists":
        // Handle duplicate user
    case "failed to create user":
        // Handle database error
    default:
        // Handle other errors
    }
}
```

## Complete Example

```go
package main

import (
    "github.com/berkkaradalan/CoreGo"
    "github.com/berkkaradalan/CoreGo/auth"
    "github.com/gin-gonic/gin"
)

func main() {
    core, _ := corego.New(&corego.Config{
        Auth: &auth.Config{
            Secret:       "super-secret-key",
            TokenExpiry:  60,
            DatabaseName: "users",
        },
    })
    defer core.Close()

    r := gin.Default()

    // Public routes
    r.POST("/signup", core.Auth.SignupHandler())
    r.POST("/login", core.Auth.LoginHandler())

    // Protected routes
    api := r.Group("/api")
    api.Use(core.Auth.Middleware())
    {
        api.GET("/me", core.Auth.GetProfileHandler())
        api.PATCH("/me", core.Auth.UpdateProfileHandler())
        api.POST("/change-password", core.Auth.ChangePasswordHandler())
        api.DELETE("/me", core.Auth.DeleteAccountHandler())
    }

    r.Run(":8080")
}
```
