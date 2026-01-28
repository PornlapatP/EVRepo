package main

import (
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
	// "github.com/pornlapatP/EV/internal/user/handler"
	// "golang.org/x/telemetry/config"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	database.Connect()

	database.DB.AutoMigrate(&models.User{})
	// log.Printf("CFG: %+v\n", cfg) //  log ตรงนี้
	authService := service.NewAuthService(cfg)
	authHandler := handler.NewAuthHandler(authService)

	//server
	r := gin.Default()
	// CORS configuration
	corsConfig := cors.Config{
		AllowOrigins: []string{
			"http://localhost:5008",
			"http://127.0.0.1:5008",
		},
		AllowMethods: []string{
			"GET", "POST", "PUT", "DELETE", "OPTIONS",
			// "GET", "POST", "PUT", "PATCH", "DELETE",
		},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
		},
		AllowCredentials: true,
	}

	r.Use(cors.New(corsConfig))

	auth := r.Group("/")
	{
		auth.GET("login", authHandler.Login)
		auth.GET("dashboard", authHandler.Callback)
		auth.GET("logout", authHandler.Logout)
		auth.POST("register", authHandler.Register)
		// auth.GET("logout", authHandler.Logout)
	}

	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(authService))
	{
		api.GET("/profile", handler.ProfileHandler(authService))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Println("Server running on :" + port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}

	// r.Run(":" + port)
}
