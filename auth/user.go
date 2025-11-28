package auth

import (
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GetUserByID finds a user by ID
func (m *Manager) GetUserByID(userID string) (*User, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	var user User
	err = m.db.FindOne(m.config.DatabaseName, bson.M{"_id": objID}, &user)
	if err != nil {
		return nil, errors.New("user not found")
	}

	user.ID = userID
	return &user, nil
}

// UpdateProfile updates user's custom fields
func (m *Manager) UpdateProfile(userID string, req UpdateProfileRequest) (*User, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	// Update custom fields
	update := bson.M{
		"$set": bson.M{
			"custom": req.Custom,
		},
	}

	err = m.db.UpdateOne(m.config.DatabaseName, bson.M{"_id": objID}, update)
	if err != nil {
		return nil, errors.New("failed to update profile")
	}

	// Return updated user
	return m.GetUserByID(userID)
}

// ChangePassword changes user password
func (m *Manager) ChangePassword(userID string, req ChangePasswordRequest) error {
	// 1. Get user
	user, err := m.GetUserByID(userID)
	if err != nil {
		return err
	}

	// 2. Verify old password
	if !VerifyPassword(user.Password, req.OldPassword) {
		return errors.New("invalid old password")
	}

	// 3. Hash new password
	hashedPassword, err := HashPassword(req.NewPassword)
	if err != nil {
		return err
	}

	// 4. Update password
	objID, _ := primitive.ObjectIDFromHex(userID)
	err = m.db.UpdateOne(
		m.config.DatabaseName,
		bson.M{"_id": objID},
		bson.M{"$set": bson.M{"password": hashedPassword}},
	)

	return err
}

// DeleteAccount deletes user account
func (m *Manager) DeleteAccount(userID string) error {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return errors.New("invalid user ID")
	}

	err = m.db.DeleteOne(m.config.DatabaseName, bson.M{"_id": objID})
	if err != nil {
		return errors.New("failed to delete account")
	}

	return nil
}
