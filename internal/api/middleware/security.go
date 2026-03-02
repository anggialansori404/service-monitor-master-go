package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// SecurityHeadersMiddleware adds standard security headers to every response.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains") // Recommended
		c.Header("Server", "")                                                       // Hide Server name

		c.Next()
	}
}

// IPRateLimiter is a simple in-memory rate limiter per IP address.
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  *sync.RWMutex
	r   rate.Limit // Requests per second
	b   int        // Burst size
}

// NewIPRateLimiter creates a new IPRateLimiter.
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}
}

// GetLimiter returns the rate limiter for a specific IP.
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.RLock()
	limiter, exists := i.ips[ip]
	i.mu.RUnlock()

	if !exists {
		i.mu.Lock()
		limiter = rate.NewLimiter(i.r, i.b)
		i.ips[ip] = limiter
		i.mu.Unlock()
	}

	return limiter
}

// GlobalRateLimiter sets a global limit of e.g. 100 requests / min / IP.
// r = 100/60 ~ 1.66 requests per second, b = 100 burst.
func GlobalRateLimiter() gin.HandlerFunc {
	limiter := NewIPRateLimiter(rate.Limit(100.0/60.0), 100) // 100 req per min

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.GetLimiter(ip).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests, please try again later",
			})
			return
		}
		c.Next()
	}
}

// LoginRateLimiter sets a strict limit of e.g. 5 requests / min / IP for brute-force protection.
// r = 5/60 ~ 0.08 requests per second, b = 5 burst.
func LoginRateLimiter() gin.HandlerFunc {
	limiter := NewIPRateLimiter(rate.Limit(5.0/60.0), 5) // 5 req per min

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !limiter.GetLimiter(ip).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many login attempts, please wait 1 minute",
			})
			return
		}
		c.Next()
	}
}
// IPBlacklist checks if the client IP is in the blacklist.
func IPBlacklist(repo repository.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		// Do not block localhost
		if ip == "127.0.0.1" || ip == "::1" {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		isBlacklisted, err := repo.IsIPBlacklisted(ctx, ip)
		if err != nil {
			log.Printf("[SECURITY ERROR] Failed to check blacklist for IP %s: %v", ip, err)
			c.Next()
			return
		}

		if isBlacklisted {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "your IP has been blacklisted due to suspicious activity",
			})
			return
		}

		c.Next()
	}
}

// SuspiciousPathLogger logs IPs attempting suspicious paths and blacklists them.
func SuspiciousPathLogger(repo repository.Repository) gin.HandlerFunc {
	suspiciousPatterns := []string{
		"index.php", ".php", ".cgi", ".env", "wp-admin", ".git",
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		// Do not block localhost
		if ip == "127.0.0.1" || ip == "::1" {
			c.Next()
			return
		}

		path := strings.ToLower(c.Request.URL.Path)
		isSuspicious := false
		reason := ""

		for _, pattern := range suspiciousPatterns {
			if strings.Contains(path, pattern) {
				isSuspicious = true
				reason = "Suspicious path access: " + c.Request.URL.Path
				break
			}
		}

		// Special case for .json at root (e.g. /config.json, /package.json) but ignore valid ones if any (none expected at root for this app)
		if !isSuspicious && strings.HasSuffix(path, ".json") && !strings.Contains(strings.TrimPrefix(path, "/"), "/") {
			isSuspicious = true
			reason = "Suspicious path access (root json): " + c.Request.URL.Path
		}

		if isSuspicious {
			log.Printf("[SECURITY] Suspicious access attempt from %s: %s %s. Blacklisting.", ip, c.Request.Method, c.Request.URL.Path)

			// Blacklist IP asynchronously
			go func(ipAddr, rsn string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := repo.BlacklistIP(ctx, ipAddr, rsn); err != nil {
					log.Printf("[SECURITY ERROR] Failed to blacklist IP %s: %v", ipAddr, err)
				}
			}(ip, reason)

			securityLog := &models.SecurityLog{
				IP:        ip,
				Path:      c.Request.URL.Path,
				Method:    c.Request.Method,
				UserAgent: c.Request.UserAgent(),
				Timestamp: time.Now(),
			}
			go func(l *models.SecurityLog) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := repo.CreateSecurityLog(ctx, l); err != nil {
					log.Printf("[SECURITY ERROR] Failed to save security log: %v", err)
				}
			}(securityLog)

			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "suspicious activity detected, your IP has been blacklisted",
			})
			return
		}

		c.Next()
	}
}
