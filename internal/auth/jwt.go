// Package auth issues and verifies the access tokens the API uses.
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer is the iss claim every token carries.
const Issuer = "go-event-api"

var (
	// ErrInvalidToken covers malformed tokens, bad signatures and wrong issuers.
	ErrInvalidToken = errors.New("invalid token")
	// ErrExpiredToken is returned once the exp claim has passed.
	ErrExpiredToken = errors.New("token expired")
)

// TokenIssuer signs and parses HS256 access tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenIssuer builds an issuer with the given signing secret and lifetime.
func NewTokenIssuer(secret string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret), ttl: ttl, now: time.Now}
}

// Issue signs a token for the user and returns it with its expiry.
func (t *TokenIssuer) Issue(userID int64) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    Issuer,
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Parse verifies a token and returns the user id in its subject.
func (t *TokenIssuer) Parse(raw string) (int64, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return t.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return 0, ErrExpiredToken
		}
		return 0, ErrInvalidToken
	}

	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidToken
	}
	return id, nil
}
