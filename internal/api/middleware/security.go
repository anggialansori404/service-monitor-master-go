package middleware

import (
	"context"
	"fmt"
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

// QuietLogger is like gin.Logger() but skips 404 responses to reduce log
// spam from bot scanners probing random paths (/.env, /wp-admin, etc).
// Security-relevant events are still recorded separately via security_logs
// in the database, so nothing important is lost.
func QuietLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		if param.StatusCode == http.StatusNotFound {
			return ""
		}
		return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s %#v\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			param.Path,
		)
	})
}

// SecurityHeaders adds standard security headers to every response.
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

// BlacklistManager manages in-memory caching of blacklisted IPs with periodic DB sync.
type BlacklistManager struct {
	repo   repository.Repository
	mu     sync.RWMutex
	ips    map[string]struct{}
	stopCh chan struct{}
	// trusted holds IPs (reverse proxy, gateway) that must never be blacklisted.
	trusted map[string]struct{}
}

// NewBlacklistManager initializes the BlacklistManager, loads all blacklisted IPs into memory,
// and starts a background periodic synchronization routine.
func NewBlacklistManager(repo repository.Repository) *BlacklistManager {
	return NewBlacklistManagerWithTrusted(repo, nil)
}

// NewBlacklistManagerWithTrusted is like NewBlacklistManager but also takes a list
// of trusted proxy IPs that are exempt from blacklisting.
func NewBlacklistManagerWithTrusted(repo repository.Repository, trustedIPs []string) *BlacklistManager {
	trusted := make(map[string]struct{}, len(trustedIPs))
	for _, ip := range trustedIPs {
		if ip = strings.TrimSpace(ip); ip != "" {
			trusted[ip] = struct{}{}
		}
	}
	if len(trusted) > 0 {
		log.Printf("[SECURITY] %d trusted proxy IP(s) exempt from blacklisting", len(trusted))
	}
	bm := &BlacklistManager{
		repo:    repo,
		ips:     make(map[string]struct{}),
		stopCh:  make(chan struct{}),
		trusted: trusted,
	}
	bm.LoadFromDB()
	go bm.startPeriodicSync(5 * time.Minute)
	return bm
}

// IsTrusted reports whether the IP is a trusted proxy exempt from blacklisting.
func (bm *BlacklistManager) IsTrusted(ip string) bool {
	_, ok := bm.trusted[ip]
	return ok
}

// LoadFromDB fetches all blacklisted IPs from database and refreshes memory cache.
func (bm *BlacklistManager) LoadFromDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ips, err := bm.repo.GetBlacklistedIPs(ctx)
	if err != nil {
		log.Printf("[SECURITY WARNING] Failed to load blacklisted IPs from DB: %v", err)
		return
	}

	bm.mu.Lock()
	newMap := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		newMap[ip] = struct{}{}
	}
	bm.ips = newMap
	count := len(bm.ips)
	bm.mu.Unlock()

	log.Printf("[SECURITY] Loaded %d blacklisted IPs into in-memory cache", count)
}

// IsBlacklisted performs an O(1) in-memory lookup to check if an IP is banned.
func (bm *BlacklistManager) IsBlacklisted(ip string) bool {
	bm.mu.RLock()
	_, found := bm.ips[ip]
	bm.mu.RUnlock()
	return found
}

// Blacklist immediately marks the IP in memory and persists it to DB asynchronously.
// Trusted proxy IPs are never blacklisted (silently skipped to avoid log spam).
func (bm *BlacklistManager) Blacklist(ip, reason string) {
	if bm.IsTrusted(ip) {
		return
	}
	bm.mu.Lock()
	bm.ips[ip] = struct{}{}
	bm.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := bm.repo.BlacklistIP(ctx, ip, reason); err != nil {
			log.Printf("[SECURITY ERROR] Failed to persist blacklisted IP %s: %v", ip, err)
		}
	}()
}

// Count returns the number of blacklisted IPs in memory.
func (bm *BlacklistManager) Count() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return len(bm.ips)
}

func (bm *BlacklistManager) startPeriodicSync(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			bm.LoadFromDB()
		case <-bm.stopCh:
			return
		}
	}
}

// Stop stops the background synchronization.
func (bm *BlacklistManager) Stop() {
	close(bm.stopCh)
}

// IPBlacklist checks if the client IP is in the blacklist using in-memory lookup.
// Trusted proxy IPs bypass the check even if present in the DB (safety net).
func IPBlacklist(bm *BlacklistManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		// Do not block localhost
		if ip == "127.0.0.1" || ip == "::1" {
			c.Next()
			return
		}

		// Never block trusted proxies
		if bm.IsTrusted(ip) {
			c.Next()
			return
		}

		if bm.IsBlacklisted(ip) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "your IP has been blacklisted due to suspicious activity",
			})
			return
		}

		c.Next()
	}
}

// SuspiciousPathLogger logs IPs attempting suspicious paths and blacklists them.
func SuspiciousPathLogger(bm *BlacklistManager, repo repository.Repository) gin.HandlerFunc {
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
			// Trusted proxies: log to DB only, don't spam stdout (bot scanners
			// hitting the proxy would flood the logs).
			if !bm.IsTrusted(ip) {
				log.Printf("[SECURITY] Suspicious access attempt from %s: %s %s. Blacklisting.", ip, c.Request.Method, c.Request.URL.Path)
			}

			// Blacklist IP immediately in-memory and asynchronously persist to DB
			// (no-op for trusted proxy IPs)
			bm.Blacklist(ip, reason)

			securityLog := &models.SecurityLog{
				IP:        ip,
				Path:      c.Request.URL.Path,
				Method:    c.Request.Method,
				UserAgent: c.Request.UserAgent(),
				Timestamp: time.Now().Local(),
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
