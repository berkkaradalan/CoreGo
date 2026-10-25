package auth

import (
	"testing"
	"time"
)

func TestUserIsBannedAt(t *testing.T) {
	now := testEpoch
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	exact := now

	tests := []struct {
		name string
		user User
		want bool
	}{
		{"not banned", User{}, false},
		{"not banned with stale expiry", User{BanExpires: &future}, false},
		{"permanent ban", User{Banned: true}, true},
		{"ban expiring in the future", User{Banned: true, BanExpires: &future}, true},
		{"ban expired in the past", User{Banned: true, BanExpires: &past}, false},
		{"ban expiring exactly now", User{Banned: true, BanExpires: &exact}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.IsBannedAt(now); got != tt.want {
				t.Errorf("IsBannedAt = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserIsBannedUsesWallClock(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	if !(&User{Banned: true, BanExpires: &future}).IsBanned() {
		t.Error("IsBanned = false for a ban expiring in an hour")
	}
	if (&User{Banned: true, BanExpires: &past}).IsBanned() {
		t.Error("IsBanned = true for a ban that expired an hour ago")
	}
}
