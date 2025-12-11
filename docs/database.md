# Database Operations

CoreGo provides a unified database interface that works across different database systems.

## Supported Databases

- **MongoDB** - Full support with unified API
- **PostgreSQL** - Full support with SQL operations
- **MySQL** - Coming soon

## Configuration

### MongoDB

```go
core, err := corego.New(&corego.Config{
    Mongo: &database.MongoConfig{
        URL:      "mongodb://localhost:27017",
        Database: "myapp",
    },
})
```

### PostgreSQL

```go
core, err := corego.New(&corego.Config{
    Postgres: &database.PostgresConfig{
        URL: "postgres://user:password@localhost:5432/myapp",
    },
})
```

### Auto-Configuration

CoreGo automatically connects to databases if environment variables are set in your `.env`:

**MongoDB:**
```env
MONGODB_CONNECTION_URL=mongodb://localhost:27017
MONGODB_DATABASE=myapp
```

**PostgreSQL:**
```env
POSTGRES_CONNECTION_URL=postgres://user:password@localhost:5432/myapp
```

```go
// No database config needed - auto-connects from .env
core, err := corego.New(&corego.Config{
    Auth: &auth.Config{...},
})
```

## CRUD Operations

### Insert One

```go
// Insert a document
doc := map[string]any{
    "name":  "John Doe",
    "email": "john@example.com",
    "age":   30,
}

id, err := core.Mongo.InsertOne("users", doc)
if err != nil {
    // Handle error
}

fmt.Println("Inserted ID:", id)
```

### Insert with Struct

```go
type User struct {
    Name  string `bson:"name"`
    Email string `bson:"email"`
    Age   int    `bson:"age"`
}

user := User{
    Name:  "Jane Doe",
    Email: "jane@example.com",
    Age:   28,
}

id, err := core.Mongo.InsertOne("users", user)
```

### Find One

```go
type User struct {
    ID    string `bson:"_id"`
    Name  string `bson:"name"`
    Email string `bson:"email"`
}

var user User
err := core.Mongo.FindOne("users", map[string]any{
    "email": "john@example.com",
}, &user)

if err != nil {
    // User not found or error
}
```

### Find Many

```go
// Find all users over 25
results, err := core.Mongo.Find("users", map[string]any{
    "age": map[string]any{"$gt": 25},
})

if err != nil {
    // Handle error
}

for _, doc := range results {
    name := doc["name"].(string)
    email := doc["email"].(string)
    fmt.Printf("User: %s (%s)\n", name, email)
}
```

### Update One

```go
err := core.Mongo.UpdateOne(
    "users",
    map[string]any{"email": "john@example.com"},
    map[string]any{
        "$set": map[string]any{
            "age": 31,
            "updated_at": time.Now(),
        },
    },
)
```

### Update Many

```go
// Update all users in a city
err := core.Mongo.UpdateMany(
    "users",
    map[string]any{"city": "New York"},
    map[string]any{
        "$set": map[string]any{
            "timezone": "EST",
        },
    },
)
```

### Delete One

```go
err := core.Mongo.DeleteOne("users", map[string]any{
    "email": "john@example.com",
})
```

### Delete Many

```go
// Delete all inactive users
err := core.Mongo.DeleteMany("users", map[string]any{
    "status": "inactive",
})
```

## Advanced Queries

### Complex Filters

```go
// Find users between 25-35 years old in specific cities
filter := map[string]any{
    "age": map[string]any{
        "$gte": 25,
        "$lte": 35,
    },
    "city": map[string]any{
        "$in": []string{"New York", "San Francisco", "Boston"},
    },
    "status": "active",
}

results, err := core.Mongo.Find("users", filter)
```

### Projections

For advanced queries, access the raw collection:

```go
collection := core.Mongo.Collection("users")

opts := options.Find().SetProjection(map[string]any{
    "name": 1,
    "email": 1,
    "_id": 0,
})

cursor, err := collection.Find(context.Background(), filter, opts)
```

## Working with Collections

### Get Raw Collection

```go
// Access MongoDB collection directly for advanced operations
collection := core.Mongo.Collection("users")

// Now you can use any MongoDB driver method
cursor, err := collection.Find(context.Background(), bson.M{})
count, err := collection.CountDocuments(context.Background(), bson.M{})
```

### Indexes

```go
collection := core.Mongo.Collection("users")

// Create an index
indexModel := mongo.IndexModel{
    Keys: bson.D{{Key: "email", Value: 1}},
    Options: options.Index().SetUnique(true),
}

_, err := collection.Indexes().CreateOne(context.Background(), indexModel)
```

## Real-World Examples

### Blog Post CRUD

```go
type BlogPost struct {
    ID        string    `bson:"_id,omitempty"`
    UserID    string    `bson:"user_id"`
    Title     string    `bson:"title"`
    Content   string    `bson:"content"`
    Tags      []string  `bson:"tags"`
    CreatedAt time.Time `bson:"created_at"`
    UpdatedAt time.Time `bson:"updated_at"`
}

// Create post
post := BlogPost{
    UserID:    userID,
    Title:     "Getting Started with CoreGo",
    Content:   "CoreGo is a powerful framework...",
    Tags:      []string{"go", "backend", "framework"},
    CreatedAt: time.Now(),
    UpdatedAt: time.Now(),
}

postID, err := core.Mongo.InsertOne("posts", post)

// Get user's posts
posts, err := core.Mongo.Find("posts", map[string]any{
    "user_id": userID,
})

// Update post
err = core.Mongo.UpdateOne(
    "posts",
    map[string]any{"_id": postID},
    map[string]any{
        "$set": map[string]any{
            "content": "Updated content...",
            "updated_at": time.Now(),
        },
    },
)

// Delete post
err = core.Mongo.DeleteOne("posts", map[string]any{
    "_id": postID,
    "user_id": userID, // Ensure user owns the post
})
```

### User Analytics

```go
// Track user activity
activity := map[string]any{
    "user_id": userID,
    "action": "page_view",
    "page": "/dashboard",
    "timestamp": time.Now(),
    "metadata": map[string]any{
        "ip": "192.168.1.1",
        "user_agent": "Mozilla/5.0...",
    },
}

_, err := core.Mongo.InsertOne("analytics", activity)

// Get user's recent activity
activities, err := core.Mongo.Find("analytics", map[string]any{
    "user_id": userID,
})
```

## Error Handling

```go
id, err := core.Mongo.InsertOne("users", doc)
if err != nil {
    if mongo.IsDuplicateKeyError(err) {
        // Handle duplicate key error
    } else {
        // Handle other errors
    }
}
```

## Best Practices

1. **Use Indexes**: Create indexes for frequently queried fields
2. **Connection Pooling**: CoreGo handles this automatically
3. **Context Timeouts**: Operations have default 5-second timeouts
4. **Error Handling**: Always check for errors
5. **Data Validation**: Validate data before inserting

---

# PostgreSQL Operations

CoreGo provides direct SQL access to PostgreSQL with pgx/v5.

## SQL Operations

PostgreSQL uses two main methods:

### Query - Returns Results

```go
// Returns []map[string]any
results, err := core.Postgres.Query(
    "SELECT * FROM users WHERE age > $1",
    25,
)

for _, row := range results {
    name := row["name"].(string)
    email := row["email"].(string)
}
```

### Exec - Returns Affected Rows

```go
// Returns affected row count
affected, err := core.Postgres.Exec(
    "UPDATE users SET age = $1 WHERE email = $2",
    31, "john@example.com",
)
```

## Basic CRUD

### Create Table

```go
_, err := core.Postgres.Exec(`
    CREATE TABLE IF NOT EXISTS users (
        id SERIAL PRIMARY KEY,
        name VARCHAR(255) NOT NULL,
        email VARCHAR(255) UNIQUE NOT NULL,
        age INT,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    )
`)
```

### Insert

```go
// Insert and get the created record with RETURNING
result, err := core.Postgres.Query(
    "INSERT INTO users (name, email, age) VALUES ($1, $2, $3) RETURNING id, name, email, created_at",
    "Jane Doe", "jane@example.com", 28,
)

userID := result[0]["id"]
```

### Query

```go
// Find one
user, err := core.Postgres.Query(
    "SELECT * FROM users WHERE email = $1 LIMIT 1",
    "john@example.com",
)

if len(user) == 0 {
    // Not found
}

// Find many
users, err := core.Postgres.Query(
    "SELECT * FROM users WHERE age > $1 ORDER BY created_at DESC",
    25,
)
```

### Update

```go
// Update with RETURNING
result, err := core.Postgres.Query(
    "UPDATE users SET age = $1 WHERE email = $2 RETURNING *",
    31, "john@example.com",
)

// Dynamic updates (only update provided fields)
result, err := core.Postgres.Query(`
    UPDATE users
    SET name = COALESCE($1, name),
        age = COALESCE($2, age)
    WHERE id = $3
    RETURNING *`,
    newName, newAge, userID,
)
```

### Delete

```go
affected, err := core.Postgres.Exec(
    "DELETE FROM users WHERE email = $1",
    "john@example.com",
)

if affected == 0 {
    // No records deleted
}
```

## Real-World Example

### Product CRUD

```go
// Create products table
_, err := core.Postgres.Exec(`
    CREATE TABLE IF NOT EXISTS products (
        id SERIAL PRIMARY KEY,
        name VARCHAR(255) NOT NULL,
        price DECIMAL(10,2) NOT NULL,
        stock INT DEFAULT 0,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    )
`)

// Create product
product, err := core.Postgres.Query(
    "INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING *",
    "Laptop", 999.99, 10,
)

// Get all products
products, err := core.Postgres.Query("SELECT * FROM products ORDER BY created_at DESC")

// Update stock
_, err = core.Postgres.Exec(
    "UPDATE products SET stock = stock - $1 WHERE id = $2",
    1, productID,
)

// Delete product
_, err = core.Postgres.Exec("DELETE FROM products WHERE id = $1", productID)
```

## Advanced Features

### Transactions

```go
pool := core.Postgres.GetPool()
tx, err := pool.Begin(context.Background())
if err != nil {
    // Handle error
}
defer tx.Rollback(context.Background())

// Execute operations
_, err = tx.Exec(context.Background(), "INSERT INTO users (name) VALUES ($1)", "John")
_, err = tx.Exec(context.Background(), "INSERT INTO orders (user_id) VALUES ($1)", userID)

// Commit
err = tx.Commit(context.Background())
```

### Joins

```go
posts, err := core.Postgres.Query(`
    SELECT
        p.id, p.title, p.content,
        u.name as author_name
    FROM posts p
    INNER JOIN users u ON p.user_id = u.id
    WHERE p.published = true
`)
```

### Raw Connection Pool

```go
pool := core.Postgres.GetPool()
rows, err := pool.Query(context.Background(), "SELECT * FROM users")
defer rows.Close()
```

## Best Practices

1. **Always use parameterized queries** (`$1, $2`) to prevent SQL injection
2. **Use RETURNING** to get data back from INSERT/UPDATE/DELETE
3. **Check affected rows** for UPDATE/DELETE operations
4. **Create indexes** for frequently queried columns
5. **Use transactions** for operations that must succeed or fail together
