package auth

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func signedToken(secret string, claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func TestVerifyTokenValid(t *testing.T) {
	secret := "test-secret"
	v := NewVerifier(VerifierConfig{JWTSecret: secret, Issuer: "issuer", Audience: "authenticated"})
	token, err := signedToken(secret, jwt.MapClaims{
		"sub": "user-1",
		"iss": "issuer",
		"aud": "authenticated",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := v.VerifyToken(context.Background(), token)
	if err != nil || principal.UserID != "user-1" {
		t.Fatalf("unexpected result: %#v %v", principal, err)
	}
}

func TestVerifyTokenInvalidSignature(t *testing.T) {
	v := NewVerifier(VerifierConfig{JWTSecret: "secret", Issuer: "issuer", Audience: "authenticated"})
	token, _ := signedToken("other-secret", jwt.MapClaims{
		"sub": "user-1", "iss": "issuer", "aud": "authenticated",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.VerifyToken(context.Background(), token); err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func TestVerifyTokenExpired(t *testing.T) {
	secret := "secret"
	v := NewVerifier(VerifierConfig{JWTSecret: secret, Issuer: "issuer", Audience: "authenticated"})
	token, _ := signedToken(secret, jwt.MapClaims{
		"sub": "user-1", "iss": "issuer", "aud": "authenticated",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	if _, err := v.VerifyToken(context.Background(), token); err == nil {
		t.Fatal("expected expired token error")
	}
}

func nilContext() interface{ Done() <-chan struct{} } {
	// jwt verification does not require context; pass background via package helper impossible here.
	return nil
}
