package auth

import (
	"errors"
	"time"

	"github.com/berkkaradalan/CoreGo/database"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Manager struct {
	config 	*Config
	db 		*database.MongoDB
}

func New(config *Config, db *database.MongoDB) (*Manager, error) {
	if config.Secret == "" {
		return nil, errors.New("auth secret is required")
	}

	if config.TokenExpiry == 0 {
		config.TokenExpiry = 60
	}

	if config.DatabaseName == "" {
		config.DatabaseName = "users"
	}

	return &Manager{
		config: config,
		db:		db,
	}, nil
}

// Signup creates a new user account
func (m *Manager) Signup(req SignupRequest) (*User, string, error) {
	// 1. Validate email and password
	if req.Email == "" {
		return nil, "", errors.New("email is required")
	}
	if req.Password == "" {
		return nil, "", errors.New("password is required")
	}

	// 2. Check if user already exists
	existingUser, _ := m.GetUserByEmail(req.Email)
	if existingUser != nil {
		return nil, "", errors.New("user with this email already exists")
	}

	// 3. Hash password
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		return nil, "", errors.New("failed to hash password")
	}

	// 4. Create user
	user := &User{
		Email:     req.Email,
		Password:  hashedPassword,
		Custom:    req.Custom,
		CreatedAt: time.Now(),
	}

	// 5. Save to database
	userID, err := m.db.InsertOne(m.config.DatabaseName, user)
	if err != nil {
		return nil, "", errors.New("failed to create user")
	}

	user.ID = userID

	// 6. Generate token
	token, err := m.GenerateToken(userID)
	if err != nil {
		return nil, "", errors.New("failed to generate token")
	}

	return user, token, nil
}

// Login authenticates a user
func (m *Manager) Login(req LoginRequest) (*User, string, error) {
	// 1. Validate input
	if req.Email == "" || req.Password == "" {
		return nil, "", errors.New("email and password are required")
	}

	// 2. Find user by email
	user, err := m.GetUserByEmail(req.Email)
	if err != nil {
		return nil, "", errors.New("invalid credentials")
	}

	// 3. Verify password
	if !VerifyPassword(user.Password, req.Password) {
		return nil, "", errors.New("invalid credentials")
	}

	// 4. Generate token
	token, err := m.GenerateToken(user.ID)
	if err != nil {
		return nil, "", errors.New("failed to generate token")
	}

	return user, token, nil
}

// GetUserByEmail finds a user by email
func (m *Manager) GetUserByEmail(email string) (*User, error) {
	users, err := m.db.Find(m.config.DatabaseName, map[string]any{"email": email})
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return nil, errors.New("user not found")
	}

	user := &User{}
	// Convert map to User struct
	if id, ok := users[0]["_id"].(primitive.ObjectID); ok {
		user.ID = id.Hex()
	}
	if email, ok := users[0]["email"].(string); ok {
		user.Email = email
	}
	if password, ok := users[0]["password"].(string); ok {
		user.Password = password
	}
	if custom, ok := users[0]["custom"].(map[string]interface{}); ok {
		user.Custom = custom
	}

	return user, nil
}