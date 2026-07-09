// Package handler wires the back-office review HTTP surface
// (/api/v1/admin/*) — every route here sits behind middleware.AuthMiddleware
// (Keycloak-only), never the citizen session.
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	adminmodel "github.com/pornlapatP/EV/internal/admin/model"
	adminservice "github.com/pornlapatP/EV/internal/admin/service"
	authservice "github.com/pornlapatP/EV/internal/auth/service"
)

type AdminHandler struct {
	svc         *adminservice.AdminService
	authService *authservice.AuthService
}

func NewAdminHandler(svc *adminservice.AdminService, authService *authservice.AuthService) *AdminHandler {
	return &AdminHandler{svc: svc, authService: authService}
}

// resolveActor re-queries Keycloak userinfo for the caller's access_token
// (already verified valid by AuthMiddleware) to attribute audit log entries
// to a real staff identity — never trusted from the request body.
func (h *AdminHandler) resolveActor(c *gin.Context) (adminservice.Actor, *authservice.KeycloakUser, bool) {
	accessToken, err := c.Cookie("access_token")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return adminservice.Actor{}, nil, false
	}
	user, err := h.authService.GetUserInfo(accessToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return adminservice.Actor{}, nil, false
	}
	name := user.HrFullNameTh
	if name == "" {
		name = user.GivenName + " " + user.FamilyName
	}
	return adminservice.Actor{Sub: user.Sub, Name: name}, user, true
}

func (h *AdminHandler) Me(c *gin.Context) {
	actor, user, ok := h.resolveActor(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, adminmodel.MeResponse{
		Name:       actor.Name,
		EmployeeId: user.HrEmployeeId,
	})
}

func (h *AdminHandler) Stats(c *gin.Context) {
	stats, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *AdminHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	resp, err := h.svc.List(c.Request.Context(), adminservice.ListFilter{
		Status:   c.Query("status"),
		Query:    c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *AdminHandler) Detail(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	resp, err := h.svc.Detail(c.Request.Context(), id)
	h.respondDetail(c, resp, err)
}

func (h *AdminHandler) Patch(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	actor, _, ok := h.resolveActor(c)
	if !ok {
		return
	}

	var req adminmodel.PatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.PatchField(c.Request.Context(), id, actor, req)
	h.respondDetail(c, resp, err)
}

func (h *AdminHandler) Checklist(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var checklist adminmodel.Checklist
	if err := c.ShouldBindJSON(&checklist); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.PatchChecklist(c.Request.Context(), id, checklist)
	h.respondDetail(c, resp, err)
}

type notesRequest struct {
	Notes string `json:"notes"`
}

func (h *AdminHandler) Notes(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var body notesRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.PatchNotes(c.Request.Context(), id, body.Notes)
	h.respondDetail(c, resp, err)
}

func (h *AdminHandler) Decision(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	actor, _, ok := h.resolveActor(c)
	if !ok {
		return
	}

	var req adminmodel.DecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.Decision(c.Request.Context(), id, actor, req)
	switch {
	case errors.Is(err, adminservice.ErrHasCriticalFlags):
		c.JSON(http.StatusConflict, gin.H{"code": "HAS_CRITICAL_FLAGS", "message": "คำขอนี้มีข้อควรระวังระดับร้ายแรงที่ยังไม่แก้ไข ไม่สามารถอนุมัติได้"})
	case errors.Is(err, adminservice.ErrInvalidTransition):
		c.JSON(http.StatusConflict, gin.H{"code": "INVALID_TRANSITION", "message": "คำขอนี้ถูกตัดสินไปแล้ว"})
	case errors.Is(err, adminservice.ErrReasonRequired):
		c.JSON(http.StatusBadRequest, gin.H{"code": "REASON_REQUIRED", "message": "กรุณาระบุเหตุผล"})
	default:
		h.respondDetail(c, resp, err)
	}
}

func (h *AdminHandler) respondDetail(c *gin.Context, resp *adminmodel.ReviewRequest, err error) {
	switch {
	case errors.Is(err, adminservice.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "registration not found"})
	case errors.Is(err, adminservice.ErrReasonRequired):
		c.JSON(http.StatusBadRequest, gin.H{"code": "REASON_REQUIRED", "message": "กรุณาระบุเหตุผลก่อนบันทึกการแก้ไข"})
	case errors.Is(err, adminservice.ErrCardMismatch):
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload does not match existing record shape"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusOK, resp)
	}
}

func parseID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}
