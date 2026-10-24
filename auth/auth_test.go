package auth

import (
	"time"
	"testing"
)

func TestPasswordPolicy(t *testing.T) {
	candidate := "short"
	got := CheckPasswordPolicy(candidate)
	if got != ErrPasswordTooShort {
		t.Fatalf("unexpected error received. expected %v got %v", ErrPasswordTooShort, got)
	}

	candidate = "valid password"
	got = CheckPasswordPolicy(candidate)
	if got != nil {
		t.Fatalf("unexpected error received. expected %v got %v", nil, got)
	}
}

func TestValidatePasswordChange(t *testing.T) {
	newPassword := "valid password"
	confirmPassword := "invalid"
	currentPassword := "current password"
	currentHash, err := GeneratePasswordHash(currentPassword)
	if err != nil {
		t.Fatalf("unexpected error generating password hash: %v", err)
	}

	_, got := ValidatePasswordChange(currentHash, newPassword, confirmPassword)
	if got != ErrPasswordMismatch {
		t.Fatalf("unexpected error received. expected %v got %v", ErrPasswordMismatch, got)
	}

	newPassword = currentPassword
	confirmPassword = newPassword
	_, got = ValidatePasswordChange(currentHash, newPassword, confirmPassword)
	if got != ErrReusedPassword {
		t.Fatalf("unexpected error received. expected %v got %v", ErrReusedPassword, got)
	}
}

func TestCompareDummyPasswordSpendsTime(t *testing.T) {
	// The dummy comparison must do real bcrypt work so the no-such-user
	// login path takes the same time as a wrong password.
	start := time.Now()
	CompareDummyPassword("anything")
	if elapsed := time.Since(start); elapsed < 5*time.Millisecond {
		t.Fatalf("dummy comparison returned too fast (%v); timing would leak username existence", elapsed)
	}
}
