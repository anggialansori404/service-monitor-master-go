package middleware

import (
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"monitoring-master/internal/logutil"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	unknownAgentLogMu   sync.Mutex
	lastUnknownAgentLog = make(map[string]time.Time)
)

func logUnknownAgent(agentID string, clientIP string) {
	unknownAgentLogMu.Lock()
	defer unknownAgentLogMu.Unlock()
	lastLog, ok := lastUnknownAgentLog[agentID]
	if !ok || time.Since(lastLog) > 5*time.Minute {
		lastUnknownAgentLog[agentID] = time.Now()
		log.Printf("[AGENT-AUTH] Unauthorized request from IP %s: AgentID '%s' not found (institution may have been deleted or agent config needs updating)", clientIP, agentID)
	}
}

// AgentAuth validates the X-Agent-ID and X-Secret-Key headers against the
// institutions table. On success it stores "InstitutionID" (uint) and
// "Institution" (*models.Institution) in the Gin context.
func AgentAuth(repo repository.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID := strings.TrimSpace(c.GetHeader("X-Agent-ID"))
		secretKey := strings.TrimSpace(c.GetHeader("X-Secret-Key"))

		if agentID == "" || secretKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing X-Agent-ID or X-Secret-Key header",
			})
			return
		}

		institution, err := repo.GetInstitutionByAgentID(c.Request.Context(), agentID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) || err.Error() == "record not found" {
				logUnknownAgent(agentID, c.ClientIP())
				logutil.Debugf("[AGENT-AUTH] Agent not found or institution deleted for AgentID: %s from IP %s", agentID, c.ClientIP())
			} else {
				log.Printf("[AGENT-AUTH] Database error checking AgentID: %s from IP %s: %v", agentID, c.ClientIP(), err)
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid agent credentials",
			})
			return
		}

		// Fallback check:
		// 1. First try constant-time string comparison (for plain text / direct matching)
		// 2. If fails, try bcrypt comparison (in case secret_key in DB was hashed)
		matched := false
		storedSecret := strings.TrimSpace(institution.SecretKey)
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
			log.Printf("[AGENT-AUTH] Secret mismatched for AgentID: %s from IP %s (received len=%d, stored len=%d)", agentID, c.ClientIP(), len(trimmedSecret), len(storedSecret))
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
