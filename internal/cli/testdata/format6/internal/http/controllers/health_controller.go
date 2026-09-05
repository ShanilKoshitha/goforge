package controllers

import (
	"net/http"

	"github.com/ShanilKoshitha/goforge/httpx"
)

type HealthController struct{}

func NewHealthController() *HealthController { return &HealthController{} }

func (controller *HealthController) Show(ctx *httpx.Context) error {
	return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
