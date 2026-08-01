package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
)

// AuthMiddleware validates JWT tokens on protected endpoints.
func AuthMiddleware(jwtService *auth.JWTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip panel static assets — only protect /api/* routes
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}

		// Extract token: try Authorization header first, then cookie
		token := ""
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}
		if token == "" {
			// Fallback to HttpOnly cookie
			cookieToken, err := c.Cookie("aura_session")
			if err == nil && cookieToken != "" {
				token = cookieToken
			}
		}

		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization"})
			return
		}

		claims, err := jwtService.ValidateToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		// Set user context
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", string(claims.Role))
		c.Next()
	}
}

// RequireRole middleware checks if user has the required role.
func RequireRole(roles ...auth.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole := auth.Role(c.GetString("role"))
		for _, r := range roles {
			if userRole == r {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
	}
}
