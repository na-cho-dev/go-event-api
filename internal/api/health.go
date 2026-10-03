package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// InfoResponse describes the service.
type InfoResponse struct {
	Name    string `json:"name" example:"go-event-api"`
	Version string `json:"version" example:"1.2.0"`
	Docs    string `json:"docs" example:"/swagger/index.html"`
	Health  string `json:"health" example:"/health"`
}

// HealthResponse reports liveness and the database check.
type HealthResponse struct {
	Status   string `json:"status" example:"ok"`
	Database string `json:"database" example:"ok"`
	Version  string `json:"version" example:"1.2.0"`
	Uptime   string `json:"uptime" example:"3h12m4s"`
}

// info answers the root path.
//
//	@Summary	Service information
//	@Tags		System
//	@Produce	json
//	@Success	200	{object}	InfoResponse
//	@Router		/ [get]
func (s *Server) info(c *gin.Context) {
	c.JSON(http.StatusOK, InfoResponse{
		Name:    "go-event-api",
		Version: s.version,
		Docs:    "/swagger/index.html",
		Health:  "/health",
	})
}

// health pings the database and reports readiness.
//
//	@Summary	Health check
//	@Tags		System
//	@Produce	json
//	@Success	200	{object}	HealthResponse
//	@Failure	503	{object}	HealthResponse
//	@Router		/health [get]
func (s *Server) health(c *gin.Context) {
	resp := HealthResponse{
		Status:   "ok",
		Database: "ok",
		Version:  s.version,
		Uptime:   time.Since(s.started).Round(time.Second).String(),
	}
	status := http.StatusOK

	if s.db != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := s.db.PingContext(ctx); err != nil {
			s.log.Warn("health: database ping failed", "error", err)
			resp.Status = "degraded"
			resp.Database = "unreachable"
			status = http.StatusServiceUnavailable
		}
	}
	c.JSON(status, resp)
}
