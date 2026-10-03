// Package api builds the HTTP server: middleware, routes and handlers.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"golang.org/x/crypto/bcrypt"

	"github.com/na-cho-dev/go-event-api/internal/auth"
	"github.com/na-cho-dev/go-event-api/internal/config"
	"github.com/na-cho-dev/go-event-api/internal/database"
)

// Pinger reports whether the database is reachable. *sql.DB satisfies it.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Server holds everything the handlers need.
type Server struct {
	cfg       config.Config
	stores    database.Stores
	tokens    *auth.TokenIssuer
	log       *slog.Logger
	db        Pinger
	version   string
	started   time.Time
	dummyHash []byte
	engine    *gin.Engine
}

// New wires the middleware and routes and returns a ready Server.
func New(cfg config.Config, stores database.Stores, tokens *auth.TokenIssuer, log *slog.Logger, db Pinger, version string) *Server {
	if cfg.IsProduction() || cfg.Env == config.EnvTest {
		gin.SetMode(gin.ReleaseMode)
	}
	useJSONFieldNames()

	// Used to equalise login timing when the email is unknown.
	dummy, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), cfg.BcryptCost)

	s := &Server{
		cfg:       cfg,
		stores:    stores,
		tokens:    tokens,
		log:       log,
		db:        db,
		version:   version,
		started:   time.Now(),
		dummyHash: dummy,
	}
	s.engine = s.buildEngine()
	return s
}

// Handler returns the http.Handler for the server.
func (s *Server) Handler() http.Handler { return s.engine }

func (s *Server) buildEngine() *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(s.cfg.TrustedProxies)
	r.HandleMethodNotAllowed = true

	r.Use(
		requestID(),
		requestLogger(s.log),
		recovery(s.log),
		securityHeaders(s.cfg.IsProduction()),
		cors(s.cfg.CORSOrigins),
		maxBody(s.cfg.MaxBodyBytes),
	)
	if s.cfg.RateLimitRPS > 0 {
		r.Use(rateLimit(newIPLimiter(s.cfg.RateLimitRPS, s.cfg.RateLimitBurst)))
	}

	r.NoRoute(func(c *gin.Context) {
		fail(c, http.StatusNotFound, CodeNotFound, "No route matches "+c.Request.Method+" "+c.Request.URL.Path+".")
	})
	r.NoMethod(func(c *gin.Context) {
		fail(c, http.StatusMethodNotAllowed, CodeNotFound, c.Request.Method+" is not allowed on "+c.Request.URL.Path+".")
	})

	r.GET("/", s.info)
	r.GET("/health", s.health)

	if s.cfg.SwaggerEnabled {
		r.GET("/swagger", func(c *gin.Context) { c.Redirect(http.StatusFound, "/swagger/index.html") })
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	}

	v1 := r.Group("/api/v1")
	{
		v1.POST("/auth/register", s.register)
		v1.POST("/auth/login", s.login)

		v1.GET("/events", s.listEvents)
		v1.GET("/events/:id", s.getEvent)
		v1.GET("/events/:id/attendees", s.listAttendees)
		v1.GET("/attendees/:id/events", s.listEventsForUser)
	}

	secured := v1.Group("")
	secured.Use(s.authenticate())
	{
		secured.GET("/auth/me", s.me)

		secured.POST("/events", s.createEvent)
		secured.PUT("/events/:id", s.updateEvent)
		secured.DELETE("/events/:id", s.deleteEvent)

		secured.POST("/events/:id/attendees", s.joinEvent)
		secured.DELETE("/events/:id/attendees", s.leaveEvent)
		secured.POST("/events/:id/attendees/:userId", s.addAttendee)
		secured.DELETE("/events/:id/attendees/:userId", s.removeAttendee)
	}

	return r
}

// internalError logs the cause and answers with a generic 500.
func (s *Server) internalError(c *gin.Context, what string, err error) {
	s.log.Error(what, slog.Any("error", err), slog.String("requestId", requestIDFrom(c)))
	fail(c, http.StatusInternalServerError, CodeInternal, "Something went wrong on our side.")
}
