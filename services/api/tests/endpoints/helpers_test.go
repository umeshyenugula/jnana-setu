package endpoints

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

func createTestJWT(t *testing.T, userID, email, secret, issuer, audience string, exp time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iss":   issuer,
		"aud":   audience,
		"exp":   exp.Unix(),
		"iat":   time.Now().Unix(),
	})
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to create test JWT: %v", err)
	}
	return tokenString
}

func createTestJWTNoSub(t *testing.T, secret, issuer, audience string, exp time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": "user@example.com",
		"iss":   issuer,
		"aud":   audience,
		"exp":   exp.Unix(),
		"iat":   time.Now().Unix(),
	})
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to create test JWT: %v", err)
	}
	return tokenString
}

func requireResponse(t *testing.T, rec *httptest.ResponseRecorder, wantCode int) map[string]any {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("status=%d body=%s want=%d", rec.Code, rec.Body.String(), wantCode)
	}
	var env api.SuccessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad success envelope: %v body=%s", err, rec.Body.String())
	}
	if !env.Success {
		t.Fatalf("expected success=true body=%s", rec.Body.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T want map[string]any", env.Data)
	}
	return data
}

func decodeSuccess(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env api.SuccessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad success envelope: %v body=%s", err, rec.Body.String())
	}
	if !env.Success {
		t.Fatalf("expected success=true body=%s", rec.Body.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T want map[string]any", env.Data)
	}
	return data
}

func requireErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantErrorCode string) {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("status=%d body=%s want=%d", rec.Code, rec.Body.String(), wantCode)
	}
	var env api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad error envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Success {
		t.Fatalf("expected success=false body=%s", rec.Body.String())
	}
	if env.Error.Code != wantErrorCode {
		t.Fatalf("error code=%q want=%q body=%s", env.Error.Code, wantErrorCode, rec.Body.String())
	}
}
