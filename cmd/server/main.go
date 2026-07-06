package main

import (
	"context"
	"log"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/pornlapatP/EV/internal/auth/config"
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

	database.DB.AutoMigrate(&models.GeneralInfo{}, &models.Charger{}, &models.Vendor{}, &models.Ev{}, &models.Employee{})
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
			"GET", "POST", "PUT", "DELETE", "OPTIONS",
		},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
		},
		AllowCredentials: true,
	}

	r.Use(cors.New(corsConfig))
	RegisService := regisservice.NewGeneralService(database.DB, peacs.New())
	regisController := controller.NewControllerHandler(RegisService, storageSvc)

	apiV1 := r.Group("/api/v1")
	{
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
	r.GET("/api/thaid/profile", handler.ThaIDProfileHandler(thaidService))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Println("Server running on :" + port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
