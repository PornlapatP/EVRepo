package main

import (
	"context"
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	adminhandler "github.com/pornlapatP/EV/internal/admin/handler"
	adminservice "github.com/pornlapatP/EV/internal/admin/service"
	"github.com/pornlapatP/EV/internal/auth/config"
	"github.com/pornlapatP/EV/internal/campaign"
	"github.com/pornlapatP/EV/internal/catalog"
	"github.com/pornlapatP/EV/internal/auth/handler"
	"github.com/pornlapatP/EV/internal/auth/service"
	"github.com/pornlapatP/EV/internal/database"
	"github.com/pornlapatP/EV/internal/middleware"
	"github.com/pornlapatP/EV/internal/models"
	"github.com/pornlapatP/EV/internal/peacs"
	regisservice "github.com/pornlapatP/EV/internal/registration/ReService"
	"github.com/pornlapatP/EV/internal/registration/controller"
	"github.com/pornlapatP/EV/internal/storage"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	database.Connect()

	database.DB.AutoMigrate(&models.GeneralInfo{}, &models.Charger{}, &models.Vendor{}, &models.Ev{}, &models.Employee{}, &models.AuditLog{}, &models.MasterEV{}, &models.MasterCharger{}, &models.Campaign{})
	rawKey := os.Getenv("KEYCLOAK_PUBLIC_KEY")
	if rawKey == "" {
		log.Fatal("KEYCLOAK_PUBLIC_KEY missing")
	}

	publicKey, err := middleware.ParseRSAPublicKey(rawKey)
	if err != nil {
		log.Fatal("invalid KEYCLOAK_PUBLIC_KEY:", err)
	}

	authService := service.NewAuthService(cfg)
	authHandler := handler.NewAuthHandler(authService, cfg, database.DB)

	// ThaID service & handler
	thaidService := service.NewThaIDService(cfg)
	thaidHandler := handler.NewThaIDHandler(thaidService, cfg)

	storageSvc, err := storage.New(context.Background())
	if err != nil {
		log.Fatal("failed to init S3 storage:", err)
	}

	//server
	r := gin.Default()
	// CORS configuration — must match the frontend's actual dev origin. `next dev`
	// defaults to :3000 but auto-increments to :3001, :3002, ... if that port is
	// already taken by another project, so allow a small range for local dev.
	corsConfig := cors.Config{
		AllowOrigins: []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"http://localhost:3001",
			"http://127.0.0.1:3001",
		},
		AllowMethods: []string{
			"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS",
		},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
		},
		AllowCredentials: true,
	}

	r.Use(cors.New(corsConfig))
	campaignService := campaign.NewService(database.DB)
	campaignHandler := campaign.NewHandler(campaignService, authService)

	RegisService := regisservice.NewGeneralService(database.DB, peacs.New(), campaignService)
	regisController := controller.NewControllerHandler(RegisService, storageSvc)

	AdminService := adminservice.NewAdminService(database.DB, storageSvc)
	adminController := adminhandler.NewAdminHandler(AdminService, authService)

	catalogController := catalog.NewController(catalog.NewService(database.DB))

	apiV1 := r.Group("/api/v1")
	{
		// EV master catalog — public reference data (no auth), cacheable. Feeds
		// the registration wizard's cascading brand→model→battery dropdowns.
		apiV1.GET("/ev-catalog", catalogController.Get)

		// Charger master catalog — same treatment as /ev-catalog. Feeds the
		// wizard's brand→model charger dropdown + spec auto-fill (master-charger-seed-plan.md §9).
		apiV1.GET("/charger-catalog", catalogController.GetChargers)

		// Registration campaign window — public (no auth), sits outside the
		// citizen group so the entry page + proxy guard can read the status
		// before login. Server-computed status; fail-closed when unset.
		apiV1.GET("/campaign", campaignHandler.Public)

		// Citizen-gated: the registration wizard's CA step is only ever reached
		// after ThaID login (Entry -> ManualLogin -> registrationForm), so
		// check-ca can safely require — and use — the citizen session too.
		citizen := apiV1.Group("/general-info")
		citizen.Use(middleware.CitizenAuthMiddleware())
		{
			citizen.GET("/check-ca", regisController.CheckCA)
			citizen.POST("", regisController.CreateWithRelations)
			citizen.GET("/me", regisController.GetMine)
		}

		staff := apiV1.Group("/general-info")
		staff.Use(middleware.AuthMiddleware(authService, publicKey))
		{
			staff.GET("", regisController.GetAll)
		}

		// Back-office review console (staff) — Keycloak-gated only, same as
		// /general-info's staff group above. Handlers: internal/admin.

		admin := apiV1.Group("/admin")
		admin.Use(middleware.AuthMiddleware(authService, publicKey))
		{
			admin.GET("/me", adminController.Me)
			admin.GET("/stats", adminController.Stats)
			admin.GET("/registrations", adminController.List)
			admin.GET("/registrations/:id", adminController.Detail)
			admin.PATCH("/registrations/:id", adminController.Patch)
			admin.PATCH("/registrations/:id/checklist", adminController.Checklist)
			admin.PATCH("/registrations/:id/notes", adminController.Notes)
			admin.POST("/registrations/:id/decision", adminController.Decision)

			// Campaign window management (staff) — get the current window to
			// prefill, patch its name/start/end.
			admin.GET("/campaign", campaignHandler.AdminGet)
			admin.PATCH("/campaign", campaignHandler.AdminUpdate)
		}
	}

	// Keycloak auth routes — full-page browser redirects, intentionally outside /api
	auth := r.Group("/")
	{
		auth.GET("login", authHandler.Login)
		auth.GET("dashboard", authHandler.Callback)
		auth.GET("logout", authHandler.Logout)
	}

	// ThaID auth routes — full-page browser redirects, intentionally outside /api
	thaid := r.Group("/thaid")
	{
		thaid.GET("/login", thaidHandler.Login)
		thaid.GET("/callback", thaidHandler.Callback)
		thaid.GET("/logout", thaidHandler.Logout)
	}

	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(authService, publicKey))
	{
		api.GET("/profile", handler.ProfileHandler(authService))
	}
	// ThaIDProfileHandler checks its own access_token cookie internally — it
	// must NOT sit behind the Keycloak-only AuthMiddleware, which cannot
	// verify DOPA-issued tokens.
	r.GET("/api/thaid/profile", handler.ThaIDProfileHandler())

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Println("Server running on :" + port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
