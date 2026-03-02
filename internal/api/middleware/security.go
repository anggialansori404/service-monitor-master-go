package middleware

import (
	"net/http"
	"sync"

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
