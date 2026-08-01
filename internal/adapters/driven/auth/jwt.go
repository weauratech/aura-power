package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTConfig holds JWT configuration.
type JWTConfig struct {
	SecretKey       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// Claims represents JWT token claims.
type Claims struct {
	UserID   string `json:"sub"`
	Username string `json:"username"`
	Role     Role   `json:"role"`
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
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "aura-power",
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(s.config.SecretKey))
	if err != nil {
		return nil, err
	}

	refreshClaims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.RefreshTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "aura-power",
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

// ValidateToken parses and validates a JWT token.
func (s *JWTService) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.config.SecretKey), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
