package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/naka-meet/sfu/pkg/api"
	"github.com/naka-meet/sfu/pkg/db"
	"github.com/naka-meet/sfu/pkg/room"
	"github.com/naka-meet/sfu/pkg/signaling"
	"github.com/naka-meet/sfu/pkg/webrtc"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type RedisPubWrapper struct {
	rdb *redis.Client
}

func (w *RedisPubWrapper) Publish(ctx context.Context, channel string, message interface{}) error {
	return w.rdb.Publish(ctx, channel, message).Err()
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		jwtSecret = []byte("nakameet-default-secret-key-12345")
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	var gormDB *gorm.DB
	dbDSN := os.Getenv("DB_DSN")
	if dbDSN != "" {
		var err error
		gormDB, err = db.InitDB(dbDSN)
		if err != nil {
			log.Printf("⚠️ PostgreSQL warning: %v (proceeding with Redis ephemeral state)", err)
		} else {
			log.Println("🐘 PostgreSQL connected & tables auto-migrated (users, rooms, recordings)")
		}
	}

	udpMinStr := os.Getenv("WEBRTC_UDP_PORT_MIN")
	udpMaxStr := os.Getenv("WEBRTC_UDP_PORT_MAX")
	udpMin, _ := strconv.ParseUint(udpMinStr, 10, 16)
	udpMax, _ := strconv.ParseUint(udpMaxStr, 10, 16)

	if udpMin == 0 {
		udpMin = 50000
	}
	if udpMax == 0 {
		udpMax = 50050
	}

	// 1. SFU Router (Pion WebRTC v4)
	router, err := webrtc.NewSFURouter(uint16(udpMin), uint16(udpMax))
	if err != nil {
		log.Fatalf("Failed to initialize SFU Router: %v", err)
	}

	// 2. Room Manager
	rm := room.NewRoomManager(rdb)

	// 3. Handlers
	signalingHandler := signaling.NewHandler(rm, router, jwtSecret)
	apiHandler := api.NewAPIHandlerWithDeps(jwtSecret, &RedisPubWrapper{rdb: rdb}, rm, gormDB)

	mux := http.NewServeMux()

	// REST API Endpoints (/api/v1/auth/login, /api/v1/rooms, /api/v1/rooms/:slug, /api/v1/rooms/:slug/live)
	mux.Handle("/api/v1/", apiHandler)


	// WebSocket Signaling Endpoint (/ws/signaling)
	mux.Handle("/ws/signaling", signalingHandler)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("🚀 SFU Backend Server running on port :%s (UDP range %d-%d)", port, udpMin, udpMax)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
