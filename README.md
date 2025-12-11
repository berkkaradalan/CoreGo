# CoreGo

A comprehensive backend framework for Go that provides production-ready authentication, multi-database support, and environment management through a unified API. Framework-agnostic and built for developer experience.

![image search api](https://berkkdev.com/corego.png)


## Features

- **🔐 Authentication System**: Built-in JWT authentication with user management
- **💾 Multi-Database Support**: Currently supports MongoDB (PostgreSQL, MySQL coming soon)
- **🎯 Framework Agnostic**: Works with any Go web framework (Gin, Echo, Fiber, etc.)
- **⚙️ Environment Management**: Seamless `.env` file integration
- **🔌 Modular Design**: Use only what you need
- **🚀 Production Ready**: Battle-tested components for real-world applications

## 📥 Installation

```bash
go get github.com/berkkaradalan/CoreGo
```

## 🚀 Quick Start

```go
package main

import (
    "github.com/berkkaradalan/CoreGo"
    "github.com/berkkaradalan/CoreGo/auth"
)

func main() {
    // Initialize CoreGo with your preferred database
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

    // Your application logic here
}
```

## 📦 Core Modules

### 🔐 Authentication
Full-featured authentication system with JWT tokens, user management, and customizable user data.

```go
// Signup
user, token, err := core.Auth.Signup(auth.SignupRequest{
    Email:    "user@example.com",
    Password: "password",
    Custom:   map[string]any{"name": "John"},
})

// Login
user, token, err := core.Auth.Login(auth.LoginRequest{
    Email:    "user@example.com",
    Password: "password",
})
```

### 💾 Database
Unified database interface supporting multiple database systems.

**Currently Supported:**
- ✅ MongoDB
- ✅ PostgreSQL

**Coming Soon:**
- 🔜 MySQL

```go
// Works the same across all database types
id, err := core.Mongo.InsertOne("collection", document)
results, err := core.Mongo.Find("collection", filter)
```

### ⚙️ Environment Variables
Automatic `.env` file loading with type-safe access.

```env
MONGODB_CONNECTION_URL=mongodb://localhost:27017
AUTH_SECRET=your-secret-key
PORT=8080
```

```go
core.Env.MONGODB_CONNECTION_URL  // Automatically loaded
```

## 🔌 Framework Integration

CoreGo is designed to work seamlessly with any Go web framework:

- 🍸 **[Gin](./docs/gin-integration.md)** - Example with Gin Web Framework
- 🎵 **Echo** - Coming soon
- ⚡ **Fiber** - Coming soon
- 🦁 **Chi** - Coming soon

## 📚 Documentation

- 🔐 **[Authentication Guide](./docs/authentication.md)** - Complete auth system documentation
- 💾 **[Database Operations](./docs/database.md)** - Database usage and examples
- ⚙️ **[Environment Variables](./docs/environment.md)** - Environment configuration
- 📖 **[API Reference](./docs/api-reference.md)** - Complete API documentation

## ⚡ Configuration

CoreGo uses a simple configuration structure:

```go
type Config struct {
    Mongo *database.MongoConfig  // Database configuration
    Auth  *auth.Config            // Authentication configuration
}
```

✨ Auto-configuration from environment variables:
- ✅ If `MONGODB_CONNECTION_URL` is set, MongoDB connects automatically
- ✅ No manual configuration needed for basic setup

## 📁 Project Structure

```
CoreGo/
├── auth/           # Authentication module
├── database/       # Database adapters
├── env/            # Environment management
├── docs/           # Documentation
└── test/           # Examples and tests
```

## 🔒 Security

- 🔐 Passwords hashed with bcrypt
- 🎫 JWT tokens with configurable expiry
- 🔑 Environment-based secrets
- ✅ Secure by default

## 💡 Examples

Check out the [test/main.go](test/main.go) for a complete working example with:
- ✅ Full authentication flow
- 🔒 Protected routes
- 💾 Database operations
- 👤 Custom user data

Run the example:
```bash
cd test
go run main.go
```

## 🗺️ Roadmap

- [x] MongoDB support
- [x] JWT authentication
- [x] User management
- [x] PostgreSQL support
- [ ] MySQL support
- [ ] Redis caching
- [ ] Role-based access control (RBAC)
- [ ] OAuth providers
- [ ] Session management

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📄 License

MIT License - see [LICENSE](LICENSE) for details

## 👨‍💻 Author

**Berk Karadalan** - [GitHub](https://github.com/berkkaradalan)

## 💬 Support

- 📚 [Documentation](./docs/)
- 🐛 [Issues](https://github.com/berkkaradalan/CoreGo/issues)
- 💭 [Discussions](https://github.com/berkkaradalan/CoreGo/discussions)
