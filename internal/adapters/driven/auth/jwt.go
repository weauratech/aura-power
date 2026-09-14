package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

// JWTConfig holds JWT configuration.
type JWTConfig struct {
	SecretKey       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// Claims represents JWT token claims.
type Claims struct {
	UserID      string    `json:"sub"`
	Username    string    `json:"username"`
	Role        Role      `json:"role"`
	AuthVersion int64     `json:"ver"`
	Type        TokenType `json:"typ"`
	jwt.RegisteredClaims
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// JWTService handles JWT token operations.
type JWTService struct {
	config JWTConfig
}

func NewJWTService(config JWTConfig) *JWTService {
	if config.SecretKey == "" {
		config.SecretKey = GenerateID() + GenerateID() // 64 char random secret
	}
	if config.AccessTokenTTL == 0 {
		config.AccessTokenTTL = time.Hour
	}
	if config.RefreshTokenTTL == 0 {
		config.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	return &JWTService{config: config}
}

// AccessTokenTTL returns the configured access token duration.
func (s *JWTService) AccessTokenTTL() time.Duration {
	return s.config.AccessTokenTTL
}

// RefreshTokenTTL returns the configured refresh token duration.
func (s *JWTService) RefreshTokenTTL() time.Duration {
	return s.config.RefreshTokenTTL
}

// GenerateTokens creates a new access + refresh token pair.
func (s *JWTService) GenerateTokens(user *User) (*TokenPair, error) {
	now := time.Now()
	expiresAt := now.Add(s.config.AccessTokenTTL)

	accessClaims := Claims{
		UserID:      user.ID,
		Username:    user.Username,
		Role:        user.Role,
		AuthVersion: user.AuthVersion,
		Type:        TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "aura-power",
			ID:        GenerateID(),
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(s.config.SecretKey))
	if err != nil {
		return nil, err
	}

	refreshClaims := Claims{
		UserID:      user.ID,
		Username:    user.Username,
		Role:        user.Role,
		AuthVersion: user.AuthVersion,
		Type:        TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.RefreshTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "aura-power",
			ID:        GenerateID(),
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString([]byte(s.config.SecretKey))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresAt:    expiresAt.Unix(),
	}, nil
}

// ValidateToken parses a JWT and requires the token purpose expected by the
// caller. Access and refresh tokens are never interchangeable.
func (s *JWTService) ValidateToken(tokenStr string, expectedType TokenType) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.config.SecretKey), nil
	}, jwt.WithIssuer("aura-power"), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid || claims.Type != expectedType {
		return nil, fmt.Errorf("invalid %s token", expectedType)
	}

	return claims, nil
}
