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
	"github.com/pornlapatP/EV/internal/auth/handler"
	"github.com/pornlapatP/EV/internal/auth/service"
	"github.com/pornlapatP/EV/internal/campaign"
	"github.com/pornlapatP/EV/internal/catalog"
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

	database.DB.AutoMigrate(&models.GeneralInfo{}, &models.Charger{}, &models.Vendor{}, &models.Ev{}, &models.Employee{}, &models.AuditLog{}, &models.MasterEV{}, &models.MasterCharger{}, &models.Campaign{}, &models.MasterRole{}, &models.MasterUser{}, &models.RulePolicy{})
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

		// Back-office review console (staff) — Keycloak-gated, then RBAC-gated
		// by the caller's dept_change_code (middleware.RBACMiddleware): operator
		// gets dashboard read + registration read/write, executive gets
		// dashboard read only. Handlers: internal/admin.
		rbacRead := func(resource string) gin.HandlerFunc {
			return middleware.RBACMiddleware(database.DB, authService, resource, "read")
		}
		rbacWrite := func(resource string) gin.HandlerFunc {
			return middleware.RBACMiddleware(database.DB, authService, resource, "write")
		}

		admin := apiV1.Group("/admin")
		admin.Use(middleware.AuthMiddleware(authService, publicKey))
		{
			admin.GET("/me", adminController.Me)
			admin.GET("/stats", rbacRead("dashboard"), adminController.Stats)
			admin.GET("/registrations", rbacRead("registration"), adminController.List)
			admin.GET("/registrations/:id", rbacRead("registration"), adminController.Detail)
			admin.PATCH("/registrations/:id", rbacWrite("registration"), adminController.Patch)
			admin.PATCH("/registrations/:id/checklist", rbacWrite("registration"), adminController.Checklist)
			admin.PATCH("/registrations/:id/notes", rbacWrite("registration"), adminController.Notes)
			admin.POST("/registrations/:id/decision", rbacWrite("registration"), adminController.Decision)
			admin.POST("/registrations/:id/claim", rbacWrite("registration"), adminController.Claim)
			admin.POST("/registrations/:id/release", rbacWrite("registration"), adminController.Release)

			// Dashboard tab — stat-card summary + Excel export of the current scope.
			admin.GET("/dashboard/summary", rbacRead("dashboard"), adminController.DashboardSummary)
			admin.GET("/dashboard/export", rbacRead("dashboard"), adminController.Export)

			// Campaign window management (staff) — RBAC resource of its own
			// ("campaign", seeded operator-only in cmd/seedrbac). PATCH here
			// opens/closes registration for every citizen at once, so it must
			// never be reachable by the view-only executive role: Keycloak-gated
			// alone was not enough.
			admin.GET("/campaign", rbacRead("campaign"), campaignHandler.AdminGet)
			admin.PATCH("/campaign", rbacWrite("campaign"), campaignHandler.AdminUpdate)
		}
	}

	// Auth routes — every one of them lives under /api so the whole backend is
	// reachable through a single ingress path (/api) and the frontend never has
	// to know an absolute backend URL. They used to sit at /login, /dashboard,
	// /logout and /thaid/* at the root, which meant five separate ingress path
	// rules: forget one and that route silently falls through to the Next.js
	// catch-all and answers 404 with nothing in any log to point at it.
	//
	// ⚠️ This group must NOT carry AuthMiddleware — these ARE the login routes.
	// The `api` group further down is Keycloak-gated; do not merge them.
	authRoutes := r.Group("/api/auth")
	{
		// Keycloak (staff) — /callback replaces the old /dashboard, which was a
		// confusing name for an OAuth callback and squatted on a URL the
		// frontend may well want for a real page.
		authRoutes.GET("/login", authHandler.Login)
		authRoutes.GET("/callback", authHandler.Callback)
		authRoutes.GET("/logout", authHandler.Logout)

		// ThaID (citizen). login/callback are full-page browser redirects;
		// logout is a plain XHR from services/auth.service.ts and answers JSON.
		authRoutes.GET("/thaid/login", thaidHandler.Login)
		authRoutes.GET("/thaid/callback", thaidHandler.Callback)
		authRoutes.GET("/thaid/logout", thaidHandler.Logout)
	}

	// ⏳ TEMPORARY — the old Keycloak callback path, kept alive only because the
	// Keycloak client still has just http://localhost:8080/dashboard registered.
	// KEYCLOAK_REDIRECT_URI must point at a path we actually serve, so until the
	// Keycloak team adds /api/auth/callback this alias is what keeps staff login
	// working locally. It is not part of the routing contract: nothing on the
	// frontend links here, and it must NOT be added to the ingress values —
	// nothing is deployed yet, so every environment starts on the new URI.
	//
	// Remove together with the KEYCLOAK_REDIRECT_URI change once the client is
	// updated; leaving it behind re-reserves /dashboard from the frontend.
	r.GET("/dashboard", authHandler.Callback)

	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(authService, publicKey))
	{
		api.GET("/profile", handler.ProfileHandler(authService))
	}
	// ThaIDProfileHandler checks its own access_token cookie internally — it
	// must NOT sit behind the Keycloak-only AuthMiddleware, which cannot
	// verify DOPA-issued tokens.
	r.GET("/api/thaid/profile", handler.ThaIDProfileHandler())

	// Backend listens on 8080, frontend on 3000 — the old 3000 fallback collided
	// with `next dev` whenever PORT was unset. Keep in sync with Dockerfile EXPOSE
	// and with containerPort/targetPort in the deployment values (DEPLOY.md D2).
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server running on :" + port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
