package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"time"

	"github.com/gin-gonic/gin"
	authservice "github.com/pornlapatP/EV/internal/auth/service"
	regisservice "github.com/pornlapatP/EV/internal/registration/ReService"
	"github.com/pornlapatP/EV/internal/registration/model"
	"github.com/pornlapatP/EV/internal/storage"
)

type RegistrationController struct {
	regisService *regisservice.GeneralService
	storageSvc   *storage.Service
}

func NewControllerHandler(regisService *regisservice.GeneralService, storageSvc *storage.Service) *RegistrationController {
	return &RegistrationController{
		regisService: regisService,
		storageSvc:   storageSvc,
	}
}

// CreateWithRelations handles POST /api/v1/general-info as multipart/form-data:
// a "data" part holding the JSON CreateGeneralInfoRequest, plus per-charger
// file parts named chargers[i][image] / chargers[i][labelImage].
func (c *RegistrationController) CreateWithRelations(ctx *gin.Context) {
	citizen := ctx.MustGet("citizen").(authservice.CitizenClaims)
	pid := citizen.PID

	jsonPart := ctx.PostForm("data")
	if jsonPart == "" {
		ctx.JSON(400, gin.H{"error": "missing \"data\" form field"})
		return
	}

	var req model.CreateGeneralInfoRequest
	if err := json.Unmarshal([]byte(jsonPart), &req); err != nil {
		ctx.JSON(400, gin.H{"error": "invalid data JSON: " + err.Error()})
		return
	}

	for i := range req.Chargers {
		imageKey, err := c.uploadChargerFile(ctx, pid, i, "image")
		if err != nil {
			ctx.JSON(400, gin.H{"error": err.Error()})
			return
		}
		labelImageKey, err := c.uploadChargerFile(ctx, pid, i, "labelImage")
		if err != nil {
			ctx.JSON(400, gin.H{"error": err.Error()})
			return
		}
		req.Chargers[i].ImageKey = imageKey
		req.Chargers[i].LabelImageKey = labelImageKey
	}

	if err := c.regisService.CreateGeneralInfoWithRelations(&req, citizen.PID); err != nil {
		ctx.JSON(500, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(201, gin.H{"message": "created successfully"})
}

func (c *RegistrationController) uploadChargerFile(ctx *gin.Context, pid string, index int, field string) (string, error) {
	fieldName := fmt.Sprintf("chargers[%d][%s]", index, field)

	fileHeader, err := ctx.FormFile(fieldName)
	if err != nil {
		return "", fmt.Errorf("missing file %q: %w", fieldName, err)
	}

	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("open %q: %w", fieldName, err)
	}
	defer file.Close()

	key := fmt.Sprintf("chargers/%s/%d-%s", pid, time.Now().UnixNano(), fileHeader.Filename)

	return c.storageSvc.Upload(ctx.Request.Context(), key, file, contentTypeOf(fileHeader))
}

func contentTypeOf(fh *multipart.FileHeader) string {
	if ct := fh.Header.Get("Content-Type"); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// GetAll handles GET /api/v1/general-info (staff/Keycloak-gated listing).
func (c *RegistrationController) GetAll(ctx *gin.Context) {
	data, err := c.regisService.GetAllGeneralInfo()
	if err != nil {
		ctx.JSON(500, gin.H{"error": err.Error()})
		return
	}

	resp := make([]model.GeneralInfoResponse, 0, len(data))
	for _, d := range data {
		resp = append(resp, ToGeneralInfoResponse(ctx.Request.Context(), d, c.storageSvc))
	}

	ctx.JSON(200, gin.H{"data": resp})
}

// GetMine handles GET /api/v1/general-info/me (citizen's own submissions).
func (c *RegistrationController) GetMine(ctx *gin.Context) {
	pid := ctx.MustGet("pid").(string)

	data, err := c.regisService.GetGeneralInfoByPID(pid)
	if err != nil {
		ctx.JSON(500, gin.H{"error": err.Error()})
		return
	}

	resp := make([]model.GeneralInfoResponse, 0, len(data))
	for _, d := range data {
		resp = append(resp, ToGeneralInfoResponse(ctx.Request.Context(), d, c.storageSvc))
	}

	ctx.JSON(200, gin.H{"data": resp})
}

// CheckCA handles GET /api/v1/general-info/check-ca?ca=... (citizen-gated —
// by the time a user reaches the registration wizard's CA step they've
// already completed ThaID login). If the CA already has a registration on
// file, that record's owner name AND its existing chargers/EVs (with
// presigned image URLs) are returned — not an error — so the wizard can show
// a read-only "you already registered this" summary before the citizen adds
// more. Otherwise PEA's real customer master data (cs-service) is queried; if
// found there, a GeneralInfo row is created immediately so the rest of the
// wizard has somewhere to attach chargers/EVs.
func (c *RegistrationController) CheckCA(ctx *gin.Context) {
	ca := ctx.Query("ca")
	if ca == "" {
		ctx.JSON(400, gin.H{"error": "ca query parameter is required"})
		return
	}

	citizen := ctx.MustGet("citizen").(authservice.CitizenClaims)

	general, err := c.regisService.CheckCA(ctx.Request.Context(), ca, citizen.PID)
	if err != nil {
		if errors.Is(err, regisservice.ErrCANotFound) {
			ctx.JSON(404, gin.H{
				"success": false,
				"code":    "CUSTOMER_NOT_FOUND",
				"message": "ไม่พบข้อมูลผู้ใช้ไฟฟ้า",
			})
			return
		}
		ctx.JSON(500, gin.H{"error": err.Error()})
		return
	}

	resp := ToGeneralInfoResponse(ctx.Request.Context(), *general, c.storageSvc)

	ctx.JSON(200, gin.H{
		"ca":        resp.Ca,
		"eligible":  true,
		"firstName": resp.FirstName,
		"lastName":  resp.LastName,
		"chargers":  resp.Chargers,
		"evs":       resp.Evs,
	})
}
