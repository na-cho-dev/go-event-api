package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/na-cho-dev/go-event-api/internal/auth"
	"github.com/na-cho-dev/go-event-api/internal/database"
)

const (
	ctxRequestID = "requestID"
	ctxUser      = "user"
	headerReqID  = "X-Request-ID"
)

func requestIDFrom(c *gin.Context) string {
	return c.GetString(ctxRequestID)
}

// requestID reads or mints a request id and echoes it on the response.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader(headerReqID))
		if id == "" || len(id) > 64 {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		c.Set(ctxRequestID, id)
		c.Writer.Header().Set(headerReqID, id)
		c.Next()
	}
}

// requestLogger writes one structured line per request.
func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		attrs := []any{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(start)),
			slog.String("ip", c.ClientIP()),
			slog.String("requestId", requestIDFrom(c)),
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("errors", c.Errors.String()))
		}

		switch {
		case status >= 500:
			log.Error("request", attrs...)
		case status >= 400:
			log.Warn("request", attrs...)
		case c.Request.URL.Path == "/health":
			log.Debug("request", attrs...)
		default:
			log.Info("request", attrs...)
		}
	}
}

// recovery turns panics into a logged 500 instead of a dropped connection.
func recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered",
					slog.Any("panic", r),
					slog.String("requestId", requestIDFrom(c)),
					slog.String("stack", string(debug.Stack())),
				)
				if !c.Writer.Written() {
					fail(c, http.StatusInternalServerError, CodeInternal, "Something went wrong on our side.")
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

// securityHeaders sets conservative browser defaults for an API.
func securityHeaders(production bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		if production {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// cors answers preflight requests and marks allowed origins.
func cors(allowed []string) gin.HandlerFunc {
	allowAll := false
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			allowAll = true
		}
		set[strings.ToLower(o)] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		_, ok := set[strings.ToLower(origin)]
		if !allowAll && !ok {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		if allowAll {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		h.Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After")
		h.Set("Access-Control-Max-Age", "600")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// maxBody caps request bodies so a client cannot exhaust memory.
func maxBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

// ipLimiter keeps one token bucket per client address.
type ipLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientBucket
	rps     rate.Limit
	burst   int
}

type clientBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPLimiter(rps float64, burst int) *ipLimiter {
	l := &ipLimiter{clients: map[string]*clientBucket{}, rps: rate.Limit(rps), burst: burst}
	go l.sweep()
	return l
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.clients[ip]
	if !ok {
		b = &clientBucket{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.clients[ip] = b
	}
	b.lastSeen = time.Now()
	return b.limiter.Allow()
}

func (l *ipLimiter) sweep() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-3 * time.Minute)
		l.mu.Lock()
		for ip, b := range l.clients {
			if b.lastSeen.Before(cutoff) {
				delete(l.clients, ip)
			}
		}
		l.mu.Unlock()
	}
}

// rateLimit rejects clients that exceed the configured request rate.
func rateLimit(l *ipLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP()) {
			c.Writer.Header().Set("Retry-After", "1")
			fail(c, http.StatusTooManyRequests, CodeRateLimited, "Too many requests. Slow down and try again.")
			return
		}
		c.Next()
	}
}

// authenticate requires a valid Bearer token and loads the user.
func (s *Server) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "An Authorization header with a Bearer token is required.")
			return
		}
		raw, found := strings.CutPrefix(header, "Bearer ")
		raw = strings.TrimSpace(raw)
		if !found || raw == "" {
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "The Authorization header must be in the form: Bearer <token>.")
			return
		}

		userID, err := s.tokens.Parse(raw)
		if err != nil {
			if errors.Is(err, auth.ErrExpiredToken) {
				fail(c, http.StatusUnauthorized, CodeTokenExpired, "The token has expired. Log in again.")
				return
			}
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "The token is invalid.")
			return
		}

		user, err := s.stores.Users.Get(c.Request.Context(), userID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				fail(c, http.StatusUnauthorized, CodeUnauthorized, "The token no longer belongs to an active account.")
				return
			}
			s.internalError(c, "load token user", err)
			return
		}

		c.Set(ctxUser, user)
		c.Next()
	}
}

// currentUser returns the authenticated user set by authenticate.
func currentUser(c *gin.Context) *database.User {
	if u, ok := c.Get(ctxUser); ok {
		if user, ok := u.(*database.User); ok {
			return user
		}
	}
	return nil
}
