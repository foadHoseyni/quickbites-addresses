package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"quickbites/backend/internal/database"
	"quickbites/backend/internal/features/addresses"
)

func main() {
	mongoURI := getenv("MONGO_URI", "mongodb://localhost:27017")
	dbName := getenv("MONGO_DB", "quickbites")
	addr := getenv("HTTP_ADDR", ":8080")

	ctx := context.Background()
	client, db, err := database.Connect(ctx, mongoURI, dbName)
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	defer client.Disconnect(ctx)

	// Wire the layers: repository -> service -> handler -> routes.
	addrRepo := addresses.NewRepository(db)
	if err := addrRepo.EnsureIndexes(ctx); err != nil {
		log.Fatalf("create indexes: %v", err)
	}
	addrHandler := addresses.NewHandler(addresses.NewService(addrRepo))

	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	api := r.Group("/api")
	addresses.RegisterRoutes(api, addrHandler)
	package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"quickbites/backend/internal/database"
	"quickbites/backend/internal/features/addresses"
	"quickbites/backend/internal/features/promocodes"
	"quickbites/backend/internal/middleware"
)

func main() {
	mongoURI := getenv("MONGO_URI", "mongodb://localhost:27017")
	dbName := getenv("MONGO_DB", "quickbites")
	addr := getenv("HTTP_ADDR", ":8080")

	ctx := context.Background()
	client, db, err := database.Connect(ctx, mongoURI, dbName)
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}
	defer client.Disconnect(ctx)

	// Wire the layers: repository -> service -> handler -> routes.
	addrRepo := addresses.NewRepository(db)
	if err := addrRepo.EnsureIndexes(ctx); err != nil {
		log.Fatalf("create indexes: %v", err)
	}
	addrHandler := addresses.NewHandler(addresses.NewService(addrRepo))

	promoRepo := promocodes.NewRepository(db)
	if err := promoRepo.EnsureIndexes(ctx); err != nil {
		log.Fatalf("create promo indexes: %v", err)
	}
	promoHandler := promocodes.NewHandler(promocodes.NewService(promoRepo))
	admins := middleware.ParseAdminIDs(os.Getenv("ADMIN_USER_IDS"))

	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	api := r.Group("/api")
	addresses.RegisterRoutes(api, addrHandler)
	promocodes.RegisterRoutes(api, promoHandler, admins)

	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}

	// Graceful shutdown: stop accepting new requests, let in-flight ones finish.
	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}


	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}

	// Graceful shutdown: stop accepting new requests, let in-flight ones finish.
	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
