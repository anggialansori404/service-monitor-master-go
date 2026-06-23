package handlers

import (
	"net/http"
	"time"

	"monitoring-master/internal/repository"
	"monitoring-master/internal/ws"

	"github.com/gin-gonic/gin"
)

var startTime = time.Now()

type HealthHandler struct {
	Repo     repository.Repository
	Registry *ws.Registry
}

func NewHealthHandler(repo repository.Repository) *HealthHandler {
	return &HealthHandler{Repo: repo}
}

// Health returns system health including uptime, DB status, and agent connections.
func (h *HealthHandler) Health(c *gin.Context) {
	dbOk := true
	dbErr := ""
	if err := h.Repo.Ping(c.Request.Context()); err != nil {
		dbOk = false
		dbErr = err.Error()
	}

	agentCount := 0
	if h.Registry != nil {
		agentCount = h.Registry.ConnectedCount()
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"uptime":       time.Since(startTime).String(),
		"db_connected": dbOk,
		"db_error":     dbErr,
		"agent_connections": agentCount,
	})
}

// Ready checks database connectivity and returns 200 if ready.
func (h *HealthHandler) Ready(c *gin.Context) {
	if err := h.Repo.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":       "not ready",
			"db_connected": false,
			"db_error":     err.Error(),
			"uptime":       time.Since(startTime).String(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":       "ready",
		"db_connected": true,
		"uptime":       time.Since(startTime).String(),
	})
}
