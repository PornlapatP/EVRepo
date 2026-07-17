package campaign

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	authservice "github.com/pornlapatP/EV/internal/auth/service"
	"github.com/pornlapatP/EV/internal/models"
)

type Handler struct {
	svc *Service
	// authService is used only to attribute a back-office update to the staff
	// member in the server log — the admin routes are already Keycloak-gated,
	// so resolution failing never blocks the update (best-effort attribution).
	authService *authservice.AuthService
}

func NewHandler(svc *Service, authService *authservice.AuthService) *Handler {
	return &Handler{svc: svc, authService: authService}
}

// campaignResponse is the public shape. StartAt/EndAt/ID are nil when no active
// campaign is configured; status is then "closed" (fail-closed). now is the
// server clock the client must trust instead of its own.
type campaignResponse struct {
	ID      *uint      `json:"id"`
	Name    string     `json:"name"`
	StartAt *time.Time `json:"startAt"`
	EndAt   *time.Time `json:"endAt"`
	Now     time.Time  `json:"now"`
	Status  string     `json:"status"`
}

func toResponse(c *models.Campaign, status models.CampaignStatus, now time.Time) campaignResponse {
	resp := campaignResponse{Now: now, Status: string(status)}
	if c != nil {
		start, end := c.StartAt, c.EndAt
		resp.ID = &c.ID
		resp.Name = c.Name
		resp.StartAt = &start
		resp.EndAt = &end
	}
	return resp
}

// errorJSON is the error envelope every handler here answers with. The frontend
// reads {code, message} (services/http-client.ts) and shows `message` verbatim to
// staff, so message must always be Thai — an English-only string reaching the UI
// violates the project's language rule. Without this shape the client falls back
// to axios's raw "Request failed with status code 500". The underlying err stays
// server-side (logged), never surfaced to the user.
func errorJSON(c *gin.Context, httpStatus int, code, message string, err error) {
	if err != nil {
		log.Printf("campaign: %s: %v", code, err)
	}
	c.JSON(httpStatus, gin.H{"code": code, "message": message})
}

// Public handles GET /api/v1/campaign — no auth (sits outside the citizen
// group). Feeds the entry page's UX gate and the proxy deep-link guard.
func (h *Handler) Public(c *gin.Context) {
	now := time.Now().UTC()
	campaign, status, err := h.svc.Status(now)
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "CAMPAIGN_FETCH_FAILED",
			"โหลดข้อมูลช่วงเวลากิจกรรมไม่สำเร็จ กรุณาลองใหม่อีกครั้ง", err)
		return
	}
	c.JSON(http.StatusOK, toResponse(campaign, status, now))
}

// AdminGet handles GET /api/v1/admin/campaign — the current window for the
// back-office settings dialog to prefill. Same shape as Public.
func (h *Handler) AdminGet(c *gin.Context) {
	h.Public(c)
}

type updateRequest struct {
	Name    string    `json:"name"`
	StartAt time.Time `json:"startAt" binding:"required"`
	EndAt   time.Time `json:"endAt" binding:"required"`
}

// AdminUpdate handles PATCH /api/v1/admin/campaign — set the window's
// name/start/end. Keycloak-gated (route level).
func (h *Handler) AdminUpdate(c *gin.Context) {
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errorJSON(c, http.StatusBadRequest, "INVALID_BODY",
			"ข้อมูลที่ส่งมาไม่ถูกต้อง กรุณาตรวจสอบเวลาเริ่มและเวลาสิ้นสุด", err)
		return
	}

	campaign, err := h.svc.Update(req.Name, req.StartAt, req.EndAt)
	if err == ErrStartNotBeforeEnd {
		errorJSON(c, http.StatusBadRequest, "INVALID_WINDOW",
			"เวลาเริ่มต้องมาก่อนเวลาสิ้นสุด", nil)
		return
	}
	if err != nil {
		errorJSON(c, http.StatusInternalServerError, "CAMPAIGN_UPDATE_FAILED",
			"บันทึกช่วงเวลากิจกรรมไม่สำเร็จ กรุณาลองใหม่อีกครั้ง", err)
		return
	}

	h.logActor(c, campaign)

	now := time.Now().UTC()
	c.JSON(http.StatusOK, toResponse(campaign, campaign.Status(now), now))
}

// logActor best-effort attributes the change to the Keycloak staff identity.
// AuditLog isn't reused here — it's foreign-keyed to a GeneralInfo (not null),
// and a campaign change belongs to no registration — so this goes to the server
// log instead of the review activity trail.
func (h *Handler) logActor(c *gin.Context, campaign *models.Campaign) {
	who := "unknown staff"
	if token, err := c.Cookie("access_token"); err == nil && h.authService != nil {
		if user, err := h.authService.GetUserInfo(token); err == nil {
			if user.HrFullNameTh != "" {
				who = user.HrFullNameTh
			} else {
				who = user.GivenName + " " + user.FamilyName
			}
		}
	}
	log.Printf("campaign: window updated by %s → %q [%s .. %s]",
		who, campaign.Name, campaign.StartAt.Format(time.RFC3339), campaign.EndAt.Format(time.RFC3339))
}
