package auth

import "time"

type Config struct {
    Secret         string
    TokenExpiry    int
    DatabaseName   string
}

type User struct {
    ID        string                 `bson:"_id,omitempty" json:"id"`
    Email     string                 `bson:"email" json:"email"`
    Password  string                 `bson:"password" json:"-"`
    Custom    map[string]interface{} `bson:"custom,omitempty" json:"custom,omitempty"`
    CreatedAt time.Time              `bson:"created_at" json:"created_at"`
}

// SignupRequest
type SignupRequest struct {
    Email    string
    Password string
    Custom   map[string]interface{}
}

// LoginRequest
type LoginRequest struct {
    Email    string
    Password string
}

// AuthResponse
type AuthResponse struct {
    User  User   `json:"user"`
    Token string `json:"token"`
}

// UpdateProfileRequest
type UpdateProfileRequest struct {
    Custom map[string]interface{} `json:"custom"`
}

// ChangePasswordRequest
type ChangePasswordRequest struct {
    OldPassword string `json:"old_password"`
    NewPassword string `json:"new_password"`
}