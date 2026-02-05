package controller

import (
	"github.com/gin-gonic/gin"
	// "github.com/pornlapatP/EV/internal/auth/service"
	// "github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/registration/model"
	regisservice "github.com/pornlapatP/EV/internal/registration/reservice"
)

type RegistrationController struct {
	regisService *regisservice.GeneralService
}

func NewControllerHandler(regisService *regisservice.GeneralService) *RegistrationController {
	return &RegistrationController{
		regisService: regisService,
	}
}
func (c *RegistrationController) CreateWithRelations(ctx *gin.Context) {
	var req model.CreateGeneralInfoRequest

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if err := c.regisService.CreateGeneralInfoWithRelations(&req); err != nil {
		ctx.JSON(500, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(201, gin.H{"message": "created successfully"})
}

// GET /general-info
// func (c *RegistrationController) GetAll(ctx *gin.Context) {
// 	result, err := c.regisService.GetAllGeneralInfo()
// 	if err != nil {
// 		ctx.JSON(500, gin.H{"error": err.Error()})
// 		return
// 	}

//		ctx.JSON(200, gin.H{"data": result})
//	}
func (c *RegistrationController) GetAll(ctx *gin.Context) {
	data, err := c.regisService.GetAllGeneralInfo()
	if err != nil {
		ctx.JSON(500, gin.H{"error": err.Error()})
		return
	}

	resp := make([]model.GeneralInfoResponse, 0, len(data))
	for _, d := range data {
		resp = append(resp, ToGeneralInfoResponse(d))
	}

	ctx.JSON(200, gin.H{"data": resp})
}

// GET /general-info/:id
// func (c *RegistrationController) GetByID(ctx *gin.Context) {
// 	idParam := ctx.Param("id")

// 	id, err := strconv.ParseUint(idParam, 10, 64)
// 	if err != nil {
// 		ctx.JSON(400, gin.H{"error": "invalid id"})
// 		return
// 	}

// 	result, err := c.regisService.GetGeneralInfoByID(uint(id))
// 	if err != nil {
// 		ctx.JSON(404, gin.H{"error": "data not found"})
// 		return
// 	}

// 	ctx.JSON(200, gin.H{"data": result})
// }
