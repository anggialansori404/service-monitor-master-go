package middleware

import (
	"crypto/subtle"
	"log"
	"net/http"
	"strings"
	"time"

	"monitoring-master/internal/logutil"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// AgentAuth validates the X-Agent-ID and X-Secret-Key headers against the
// institutions table. On success it stores "InstitutionID" (uint) and
// "Institution" (*models.Institution) in the Gin context.
func AgentAuth(repo repository.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := c.GetHeader("X-Agent-ID")
		secretKey := c.GetHeader("X-Secret-Key")

		if agentID == "" || secretKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing X-Agent-ID or X-Secret-Key header",
			})
			return
		}

		institution, err := repo.GetInstitutionByAgentID(c.Request.Context(), agentID)
		if err != nil {
			log.Printf("MONGODB AUTH FAILED: agent not found for AgentID: %s (err: %v)", agentID, err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid agent credentials",
			})
			return
		}

		// Fallback check:
		// 1. First try constant-time string comparison (for plain text / direct matching)
		// 2. If fails, try bcrypt comparison (in case secret_key in DB was hashed)
		matched := false
		storedSecret := institution.SecretKey
		trimmedSecret := strings.TrimSpace(secretKey)

		// Exact match check (prevents subtle length mismatch issues)
		if storedSecret == trimmedSecret {
			matched = true
		} else if subtle.ConstantTimeCompare([]byte(storedSecret), []byte(trimmedSecret)) == 1 {
			matched = true
		} else if strings.HasPrefix(storedSecret, "$2a$") || strings.HasPrefix(storedSecret, "$2b$") || strings.HasPrefix(storedSecret, "$2y$") {
			// Try bcrypt check if stored key looks like a hash
			if err := bcrypt.CompareHashAndPassword([]byte(storedSecret), []byte(trimmedSecret)); err == nil {
				matched = true
			}
		}

		if !matched {
			log.Printf("MONGODB AUTH FAILED: secret mismatched for AgentID: %s", agentID)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid agent credentials",
			})
			return
		}
		logutil.Debugf("AUTH: Agent=%s, DB_IsActive=%v", institution.AgentID, institution.IsActive)

		if !institution.IsActive {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "institution is deactivated",
			})
			return
		}

		c.Set("InstitutionID", institution.ID)
		c.Set("Institution", institution)
		c.Next()
	}
}

// JWTAuth validates the Authorization header (Bearer token) using the provided secret.
// On success it stores "UserRole" (string) in the Gin context.
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Authorization header",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid Authorization header format, expected 'Bearer <token>'",
			})
			return
		}

		tokenString := parts[1]

		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token claims",
			})
			return
		}

		role, _ := claims["role"].(string)
		userID, _ := claims["sub"].(string)
		email, _ := claims["email"].(string)

		// 1. Log isi claims asli dari JWT (hanya jika debug log aktif)
		logutil.Debugf("JWT CLAIMS: %+v", claims)

		// 2. Ekstrak Email secara aman dan log warning jika kosong
		if email == "" {
			log.Println("WARNING: JWT token does not contain 'email' claim or is empty!")
		}

		// 3. SET ke Gin Context
		logutil.Debugf("MIDDLEWARE SET: email='%s'", email)
		c.Set("UserRole", role)
		c.Set("userID", userID)
		c.Set("userEmail", email)
		c.Next()
	}
}

// GenerateJWT creates a signed JWT token with the given role and a 24-hour expiry.
func GenerateJWT(userID string, email string, role string, secret string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"role":  role,
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
