package catalog

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller {
	return &Controller{svc: svc}
}

// Get handles GET /api/v1/ev-catalog — public reference data, cacheable.
func (c *Controller) Get(ctx *gin.Context) {
	data, err := c.svc.GetCatalog()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.Header("Cache-Control", "public, max-age=3600")
	ctx.JSON(http.StatusOK, gin.H{"data": data})
}
