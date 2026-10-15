package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Principal struct {
	UserID    string
	Email     string
	Claims    map[string]any
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

type VerifierConfig struct {
	URL          string
	JWTSecret    string
	Issuer       string
	Audience     string
	JWKSURL      string
	HTTPClient   *http.Client
}

type Verifier struct {
	cfg      VerifierConfig
	issuer   string
	audience string
	http     *http.Client
	cache    *jwksCache
}

type jwksCache struct {
	mu     sync.RWMutex
	url    string
	keys   map[string]any
	expires time.Time
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Crv string `json:"crv"`
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

func NewVerifier(cfg VerifierConfig) *Verifier {
	issuer := cfg.Issuer
	if issuer == "" && cfg.URL != "" {
		issuer = strings.TrimRight(cfg.URL, "/") + "/auth/v1"
	}
	audience := cfg.Audience
	if audience == "" {
		audience = "authenticated"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	jwksURL := cfg.JWKSURL
	if jwksURL == "" && cfg.URL != "" {
		jwksURL = strings.TrimRight(cfg.URL, "/") + "/auth/v1/.well-known/jwks.json"
	}
	return &Verifier{
		cfg:      cfg,
		issuer:   issuer,
		audience: audience,
		http:     httpClient,
		cache:    &jwksCache{url: jwksURL, keys: map[string]any{}},
	}
}

func (v *Verifier) VerifyToken(ctx context.Context, raw string) (Principal, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256", "RS256", "ES256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
	)
	claims := map[string]any{}
	token, err := parser.ParseWithClaims(raw, jwt.MapClaims(claims), func(t *jwt.Token) (any, error) {
		return v.keyForToken(ctx, t)
	})
	if err != nil || !token.Valid {
		return Principal{}, fmt.Errorf("invalid token: %w", err)
	}
	mc := token.Claims.(jwt.MapClaims)
	sub, _ := mc["sub"].(string)
	if sub == "" {
		return Principal{}, fmt.Errorf("missing subject")
	}
	principal := Principal{UserID: sub, Claims: map[string]any(mc)}
	if email, ok := mc["email"].(string); ok {
		principal.Email = email
	}
	return principal, nil
}

func (v *Verifier) keyForToken(ctx context.Context, token *jwt.Token) (any, error) {
	if hm, ok := token.Method.(*jwt.SigningMethodHMAC); ok && hm.Name == "HS256" {
		if v.cfg.JWTSecret == "" {
			return nil, fmt.Errorf("missing JWT secret")
		}
		return []byte(v.cfg.JWTSecret), nil
	}
	if token.Method.Alg() == "HS256" {
		if v.cfg.JWTSecret == "" {
			return nil, fmt.Errorf("missing JWT secret")
		}
		return []byte(v.cfg.JWTSecret), nil
	}
	kid, _ := token.Header["kid"].(string)
	key, ok, err := v.cachedKey(ctx, kid, token.Method.Alg())
	if err != nil {
		return nil, err
	}
	if ok {
		return key, nil
	}
	// Force one refresh for a newly presented key ID.
	if err := v.refreshJWKS(ctx); err != nil {
		return nil, err
	}
	key, ok, err = v.cachedKey(ctx, kid, token.Method.Alg())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("signing key not found")
	}
	return key, nil
}

func (v *Verifier) cachedKey(ctx context.Context, kid, alg string) (any, bool, error) {
	v.cache.mu.RLock()
	defer v.cache.mu.RUnlock()
	if time.Now().After(v.cache.expires) {
		return nil, false, nil
	}
	if kid == "" && len(v.cache.keys) == 1 {
		for _, key := range v.cache.keys {
			return key, true, nil
		}
	}
	key, ok := v.cache.keys[kid]
	return key, ok, nil
}

func (v *Verifier) refreshJWKS(ctx context.Context) error {
	if v.cache.url == "" {
		return fmt.Errorf("missing JWKS URL")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, v.cache.url, nil)
	resp, err := v.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS request failed: %s", resp.Status)
	}
	var doc jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}
	keys := map[string]any{}
	for _, j := range doc.Keys {
		key, err := publicJWKKey(j)
		if err != nil {
			continue
		}
		id := j.Kid
		if id == "" {
			id = fmt.Sprintf("%s-%d", j.Kty, len(keys))
		}
		keys[id] = key
	}
	v.cache.mu.Lock()
	defer v.cache.mu.Unlock()
	v.cache.keys = keys
	v.cache.expires = time.Now().Add(time.Hour)
	return nil
}

func publicJWKKey(j jwk) (any, error) {
	switch j.Kty {
	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(j.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(j.E)
		if err != nil {
			return nil, err
		}
		n := new(big.Int).SetBytes(nBytes)
		e := 0
		for _, b := range eBytes {
			e = e*256 + int(b)
		}
		return &rsa.PublicKey{N: n, E: e}, nil
	case "EC":
		xBytes, err := base64.RawURLEncoding.DecodeString(j.X)
		if err != nil {
			return nil, err
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(j.Y)
		if err != nil {
			return nil, err
		}
		x := new(big.Int).SetBytes(xBytes)
		y := new(big.Int).SetBytes(yBytes)
		var curve elliptic.Curve
		switch j.Crv {
		case "", "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("unsupported curve %s", j.Crv)
		}
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
	default:
		return nil, fmt.Errorf("unsupported key type %s", j.Kty)
	}
}
