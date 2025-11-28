# Database Operations

CoreGo provides a unified database interface that works across different database systems.

## Supported Databases

- **MongoDB** - Currently supported
- **PostgreSQL** - Coming soon
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

### Auto-Configuration

CoreGo automatically connects to MongoDB if `MONGODB_CONNECTION_URL` is set in your `.env`:

```env
MONGODB_CONNECTION_URL=mongodb://localhost:27017
MONGODB_DATABASE=myapp
```

```go
// No Mongo config needed - auto-connects from .env
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

## Future Database Support

CoreGo will support multiple databases with the same API:

```go
// MongoDB
id, err := core.Mongo.InsertOne("users", doc)

// PostgreSQL (Coming soon)
id, err := core.Postgres.InsertOne("users", doc)

// MySQL (Coming soon)
id, err := core.MySQL.InsertOne("users", doc)
```

The API remains consistent across all database types.
