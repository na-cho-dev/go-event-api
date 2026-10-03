package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const secret = "a-test-secret-with-plenty-of-length-for-hs256"

func TestIssueAndParse(t *testing.T) {
	issuer := NewTokenIssuer(secret, time.Hour)
	token, exp, err := issuer.Issue(42)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if time.Until(exp) < 59*time.Minute {
		t.Fatalf("unexpected expiry %v", exp)
	}
	id, err := issuer.Parse(token)
	if err != nil || id != 42 {
		t.Fatalf("Parse = %d, %v", id, err)
	}
}

func TestParseRejectsExpired(t *testing.T) {
	token, _, err := NewTokenIssuer(secret, -time.Minute).Issue(7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewTokenIssuer(secret, time.Hour).Parse(token); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestParseRejectsOtherSecretIssuerAndAlgorithm(t *testing.T) {
	issuer := NewTokenIssuer(secret, time.Hour)

	forged, _, _ := NewTokenIssuer("another-secret-that-is-long-enough-too", time.Hour).Issue(1)
	if _, err := issuer.Parse(forged); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong secret: expected ErrInvalidToken, got %v", err)
	}

	wrongIssuer, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "someone-else",
		Subject:   "1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString([]byte(secret))
	if _, err := issuer.Parse(wrongIssuer); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong issuer: expected ErrInvalidToken, got %v", err)
	}

	noExpiry, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:  Issuer,
		Subject: "1",
	}).SignedString([]byte(secret))
	if _, err := issuer.Parse(noExpiry); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("missing exp: expected ErrInvalidToken, got %v", err)
	}

	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Issuer:    Issuer,
		Subject:   "1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := issuer.Parse(none); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("alg none: expected ErrInvalidToken, got %v", err)
	}

	badSubject, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    Issuer,
		Subject:   "not-a-number",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString([]byte(secret))
	if _, err := issuer.Parse(badSubject); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("bad subject: expected ErrInvalidToken, got %v", err)
	}
}
