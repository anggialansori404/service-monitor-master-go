package handlers

import (
	"net/http"

	"monitoring-master/internal/repository"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	Repo repository.Repository
}

func NewHealthHandler(repo repository.Repository) *HealthHandler {
	return &HealthHandler{Repo: repo}
}

// Health returns 200 OK if the process is alive.
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready checks database connectivity and returns 200 if ready.
func (h *HealthHandler) Ready(c *gin.Context) {
	if err := h.Repo.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
