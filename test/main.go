package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/berkkaradalan/GoCore/auth"
	gocore "github.com/berkkaradalan/GoCore"
	"github.com/gin-gonic/gin"
)

// getEnv gets environment variable with a default fallback
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	// Initialize CoreGo
	// MongoDB will auto-load from .env (MONGODB_CONNECTION_URL)
	// We only need to provide Auth config
	core, err := gocore.New(&gocore.Config{
		// Mongo: nil means use MONGODB_CONNECTION_URL from .env
		Auth: &auth.Config{
			Secret:       getEnv("AUTH_SECRET", "super-secret-key-for-testing"),
			TokenExpiry:  60,
			DatabaseName: getEnv("AUTH_DATABASE", "users"),
		},
	})

	if err != nil {
		log.Fatal("Failed to initialize CoreGo:", err)
	}
	defer core.Close()

	log.Println("✅ CoreGo initialized successfully!")
	log.Printf("📦 MongoDB connected to database: %s", getEnv("MONGODB_DATABASE", "corego_test"))
	log.Println("🔐 Auth system ready")

	// Initialize Gin router
	r := gin.Default()

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":   "healthy",
			"database": "connected",
			"auth":     "enabled",
		})
	})

	// Public auth routes (no authentication required)
	authGroup := r.Group("/auth")
	{
		authGroup.POST("/signup", core.Auth.SignupHandler())
		authGroup.POST("/login", core.Auth.LoginHandler())
	}

	// Protected auth routes (authentication required)
	authProtected := r.Group("/auth")
	authProtected.Use(core.Auth.Middleware())
	{
		authProtected.GET("/me", core.Auth.GetProfileHandler())
		authProtected.PATCH("/me", core.Auth.UpdateProfileHandler())
		authProtected.POST("/change-password", core.Auth.ChangePasswordHandler())
		authProtected.DELETE("/me", core.Auth.DeleteAccountHandler())
	}

	// Protected API routes
	api := r.Group("/api")
	api.Use(core.Auth.Middleware())
	{
		// Dashboard endpoint
		api.GET("/dashboard", func(c *gin.Context) {
			userID, _ := c.Get("userID")

			// Get user details
			user, err := core.Auth.GetUserByID(userID.(string))
			if err != nil {
				c.JSON(500, gin.H{"error": "failed to get user"})
				return
			}

			c.JSON(200, gin.H{
				"message": "Welcome to your dashboard!",
				"user": gin.H{
					"id":    user.ID,
					"email": user.Email,
					"custom": user.Custom,
				},
			})
		})

		// Profile stats endpoint
		api.GET("/profile/stats", func(c *gin.Context) {
			userID, _ := c.Get("userID")

			c.JSON(200, gin.H{
				"user_id": userID,
				"stats": gin.H{
					"login_count":    42,
					"last_login":     "2025-11-28T10:00:00Z",
					"account_status": "active",
				},
			})
		})

		// Custom data endpoint (MongoDB direct usage example)
		api.POST("/data", func(c *gin.Context) {
			userID, _ := c.Get("userID")

			var payload struct {
				Title   string `json:"title"`
				Content string `json:"content"`
			}

			if err := c.BindJSON(&payload); err != nil {
				c.JSON(400, gin.H{"error": "invalid payload"})
				return
			}

			// Insert custom data to MongoDB
			data := map[string]any{
				"user_id": userID,
				"title":   payload.Title,
				"content": payload.Content,
			}

			dataID, err := core.Mongo.InsertOne("user_data", data)
			if err != nil {
				c.JSON(500, gin.H{"error": "failed to save data"})
				return
			}

			c.JSON(201, gin.H{
				"message": "data saved successfully",
				"data_id": dataID,
			})
		})

		// Get user's custom data
		api.GET("/data", func(c *gin.Context) {
			userID, _ := c.Get("userID")

			// Find user's data in MongoDB
			results, err := core.Mongo.Find("user_data", map[string]any{
				"user_id": userID,
			})

			if err != nil {
				c.JSON(500, gin.H{"error": "failed to fetch data"})
				return
			}

			c.JSON(200, gin.H{
				"count": len(results),
				"data":  results,
			})
		})
	}

	// Start server
	port := fmt.Sprintf(":%s", getEnv("PORT", "8080"))
	log.Printf("\n🚀 Server starting on http://localhost%s\n", port)

	if err := r.Run(port); err != nil && err != http.ErrServerClosed {
		log.Fatal("Failed to start server:", err)
	}
}
