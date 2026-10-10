package auth

import (
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "s3cret" || hash[:len(PasswordHashPrefix)] != PasswordHashPrefix {
		t.Fatalf("password not hashed: %q", hash)
	}
	if !VerifyPassword(hash, "s3cret") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("s3cret", "s3cret") {
		t.Fatal("a plaintext stored value must never match")
	}
	if VerifyPassword("", "") {
		t.Fatal("empty credentials must not match")
	}
}

func TestHashPasswordIsIdempotent(t *testing.T) {
	hash, _ := HashPassword("x")
	again, err := HashPassword(hash)
	if err != nil || again != hash {
		t.Fatalf("re-hash changed the value: %q -> %q (%v)", hash, again, err)
	}
	empty, err := HashPassword("")
	if err != nil || empty != "" {
		t.Fatalf("empty password: %q (%v)", empty, err)
	}
}

func TestSessions(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := NewSessions(time.Hour)
	s.now = func() time.Time { return now }

	token := s.Create()
	if token == "" || !s.Valid(token) {
		t.Fatal("a fresh session must be valid")
	}
	if s.Valid("nope") {
		t.Fatal("an unknown token must be invalid")
	}

	now = now.Add(2 * time.Hour)
	if s.Valid(token) {
		t.Fatal("an expired session must be invalid")
	}

	fresh := s.Create()
	s.Drop(fresh)
	if s.Valid(fresh) {
		t.Fatal("a dropped session must be invalid")
	}
}
