package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	corego "github.com/berkkaradalan/CoreGo"
	"github.com/berkkaradalan/CoreGo/database"
	"github.com/gin-gonic/gin"
)

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	core, err := corego.New(&corego.Config{
		Postgres: &database.PostgresConfig{
			URL: getEnv("POSTGRES_URL", "postgres://corego:corego123@localhost:5432/corego_test"),
		},
	})
	if err != nil {
		log.Fatal("Failed to initialize CoreGo:", err)
	}
	defer core.Close()

	log.Println("✅ CoreGo initialized with PostgreSQL!")

	// Create products table if not exists
	_, err = core.Postgres.Exec(`
		CREATE TABLE IF NOT EXISTS products (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			price DECIMAL(10,2),
			stock INT DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal("Failed to create products table:", err)
	}
	log.Println("✅ Products table ready!")

	log.Println("✅ CoreGo initialized with PostgreSQL!")

	r := gin.Default()

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "database": "postgres"})
	})

	// --- PRODUCTS CRUD ---

	// Create product
	r.POST("/products", func(c *gin.Context) {
		var payload struct {
			Name  string  `json:"name" binding:"required"`
			Price float64 `json:"price" binding:"required"`
			Stock int     `json:"stock"`
		}

		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		result, err := core.Postgres.Query(
			"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING id, name, price, stock, created_at",
			payload.Name, payload.Price, payload.Stock,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(201, gin.H{"message": "product created", "product": result[0]})
	})

	// Get all products
	r.GET("/products", func(c *gin.Context) {
		products, err := core.Postgres.Query("SELECT * FROM products ORDER BY created_at DESC")
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{"count": len(products), "products": products})
	})

	// Get single product
	r.GET("/products/:id", func(c *gin.Context) {
		id := c.Param("id")

		result, err := core.Postgres.Query("SELECT * FROM products WHERE id = $1 LIMIT 1", id)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if len(result) == 0 {
			c.JSON(404, gin.H{"error": "product not found"})
			return
		}

		c.JSON(200, gin.H{"product": result[0]})
	})

	// Update product
	r.PATCH("/products/:id", func(c *gin.Context) {
		id := c.Param("id")

		var payload struct {
			Name  *string  `json:"name"`
			Price *float64 `json:"price"`
			Stock *int     `json:"stock"`
		}

		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		// Dynamic update query
		result, err := core.Postgres.Query(
			`UPDATE products 
			 SET name = COALESCE($1, name), 
			     price = COALESCE($2, price), 
			     stock = COALESCE($3, stock) 
			 WHERE id = $4 
			 RETURNING id, name, price, stock, created_at`,
			payload.Name, payload.Price, payload.Stock, id,
		)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if len(result) == 0 {
			c.JSON(404, gin.H{"error": "product not found"})
			return
		}

		c.JSON(200, gin.H{"message": "product updated", "product": result[0]})
	})

	// Delete product
	r.DELETE("/products/:id", func(c *gin.Context) {
		id := c.Param("id")

		affected, err := core.Postgres.Exec("DELETE FROM products WHERE id = $1", id)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		if affected == 0 {
			c.JSON(404, gin.H{"error": "product not found"})
			return
		}

		c.JSON(200, gin.H{"message": "product deleted"})
	})

	port := fmt.Sprintf(":%s", getEnv("PORT", "8080"))
	log.Printf("🚀 Server starting on http://localhost%s\n", port)

	if err := r.Run(port); err != nil && err != http.ErrServerClosed {
		log.Fatal("Failed to start server:", err)
	}
}