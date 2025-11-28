package auth

import "github.com/gin-gonic/gin"

// SignupHandler returns Gin handler for signup
func (m *Manager) SignupHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        var req SignupRequest
        if err := c.BindJSON(&req); err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }
        
        user, token, err := m.Signup(req)
        if err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }
        
        c.JSON(201, AuthResponse{User: *user, Token: token})
    }
}

// LoginHandler returns Gin handler for login
func (m *Manager) LoginHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        var req LoginRequest
        if err := c.BindJSON(&req); err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }

        user, token, err := m.Login(req)
        if err != nil {
            c.JSON(401, gin.H{"error": "invalid credentials"})
            return
        }

        c.JSON(200, AuthResponse{User: *user, Token: token})
    }
}

// GetProfileHandler returns current user profile
func (m *Manager) GetProfileHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        // User ID comes from middleware
        userID, exists := c.Get("userID")
        if !exists {
            c.JSON(401, gin.H{"error": "unauthorized"})
            return
        }

        user, err := m.GetUserByID(userID.(string))
        if err != nil {
            c.JSON(404, gin.H{"error": "user not found"})
            return
        }

        c.JSON(200, user)
    }
}

// UpdateProfileHandler updates user profile
func (m *Manager) UpdateProfileHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        userID, exists := c.Get("userID")
        if !exists {
            c.JSON(401, gin.H{"error": "unauthorized"})
            return
        }

        var req UpdateProfileRequest
        if err := c.BindJSON(&req); err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }

        user, err := m.UpdateProfile(userID.(string), req)
        if err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }

        c.JSON(200, user)
    }
}

// ChangePasswordHandler changes user password
func (m *Manager) ChangePasswordHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        userID, exists := c.Get("userID")
        if !exists {
            c.JSON(401, gin.H{"error": "unauthorized"})
            return
        }

        var req ChangePasswordRequest
        if err := c.BindJSON(&req); err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }

        err := m.ChangePassword(userID.(string), req)
        if err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }

        c.JSON(200, gin.H{"message": "password changed successfully"})
    }
}

// DeleteAccountHandler deletes user account
func (m *Manager) DeleteAccountHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        userID, exists := c.Get("userID")
        if !exists {
            c.JSON(401, gin.H{"error": "unauthorized"})
            return
        }

        err := m.DeleteAccount(userID.(string))
        if err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }

        c.JSON(200, gin.H{"message": "account deleted successfully"})
    }
}