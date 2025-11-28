# Gin Framework Integration

Complete guide for integrating CoreGo with the Gin Web Framework.

## Setup

```go
package main

import (
    "github.com/berkkaradalan/CoreGo"
    "github.com/berkkaradalan/CoreGo/auth"
    "github.com/gin-gonic/gin"
)

func main() {
    // Initialize CoreGo
    core, err := corego.New(&corego.Config{
        Auth: &auth.Config{
            Secret:       "your-jwt-secret",
            TokenExpiry:  60,
            DatabaseName: "users",
        },
    })
    if err != nil {
        panic(err)
    }
    defer core.Close()

    r := gin.Default()

    // Setup routes
    setupRoutes(r, core)

    r.Run(":8080")
}
```

## Public Routes

```go
func setupRoutes(r *gin.Engine, core *corego.Core) {
    // Health check
    r.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "healthy"})
    })

    // Auth routes
    auth := r.Group("/auth")
    {
        auth.POST("/signup", core.Auth.SignupHandler())
        auth.POST("/login", core.Auth.LoginHandler())
    }
}
```

## Protected Routes

```go
func setupRoutes(r *gin.Engine, core *corego.Core) {
    // Protected routes
    api := r.Group("/api")
    api.Use(core.Auth.Middleware())
    {
        api.GET("/profile", core.Auth.GetProfileHandler())
        api.PATCH("/profile", core.Auth.UpdateProfileHandler())
        api.POST("/change-password", core.Auth.ChangePasswordHandler())
        api.DELETE("/account", core.Auth.DeleteAccountHandler())
    }
}
```

## Custom Handlers with Auth

```go
api.GET("/dashboard", func(c *gin.Context) {
    // Get authenticated user ID
    userID, exists := c.Get("userID")
    if !exists {
        c.JSON(401, gin.H{"error": "unauthorized"})
        return
    }

    // Get user details
    user, err := core.Auth.GetUserByID(userID.(string))
    if err != nil {
        c.JSON(500, gin.H{"error": "failed to get user"})
        return
    }

    c.JSON(200, gin.H{
        "user": user,
        "message": "Welcome to your dashboard",
    })
})
```

## Database Operations in Handlers

```go
api.POST("/posts", func(c *gin.Context) {
    userID, _ := c.Get("userID")

    var req struct {
        Title   string `json:"title" binding:"required"`
        Content string `json:"content" binding:"required"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // Insert post into database
    postID, err := core.Mongo.InsertOne("posts", map[string]any{
        "user_id": userID,
        "title":   req.Title,
        "content": req.Content,
        "created_at": time.Now(),
    })

    if err != nil {
        c.JSON(500, gin.H{"error": "failed to create post"})
        return
    }

    c.JSON(201, gin.H{
        "message": "post created",
        "post_id": postID,
    })
})
```

## API Endpoints

### POST /auth/signup
```bash
curl -X POST http://localhost:8080/auth/signup \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password123",
    "custom": {
      "name": "John Doe"
    }
  }'
```

### POST /auth/login
```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "password123"
  }'
```

### GET /api/profile
```bash
curl -X GET http://localhost:8080/api/profile \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

### PATCH /api/profile
```bash
curl -X PATCH http://localhost:8080/api/profile \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "custom": {
      "name": "Jane Doe",
      "bio": "Software Engineer"
    }
  }'
```

## Complete Example

See [test/main.go](../test/main.go) for a full working example.
