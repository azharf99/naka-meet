package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

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

	jwtSecretStr := os.Getenv("JWT_SECRET")
	if jwtSecretStr == "" {
		// Fail closed: a well-known fallback secret checked into source
		// would let anyone forge a host JWT for any room.
		log.Fatal("JWT_SECRET environment variable is required and must not be empty")
	}
	jwtSecret := []byte(jwtSecretStr)

	// ALLOWED_ORIGINS is the browser-facing origin allowlist for CORS and
	// WS-upgrade checks — distinct from FRONTEND_URL (the container-internal
	// address the egress worker uses to reach the frontend), since browsers
	// never see that Docker-internal hostname. Defaults to the frontend's
	// documented local dev/deploy port.
	originsEnv := os.Getenv("ALLOWED_ORIGINS")
	if originsEnv == "" {
		originsEnv = "http://localhost:3000"
	}
	allowedOrigins := resolveAllowedOrigins(originsEnv, os.Getenv("FRONTEND_URL"))

	secureCookies := os.Getenv("COOKIE_SECURE") == "true"

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
	signalingHandler.SetAllowedOrigins(allowedOrigins)

	apiHandler := api.NewAPIHandlerWithDeps(jwtSecret, &RedisPubWrapper{rdb: rdb}, rm, gormDB)
	apiHandler.SetAllowedOrigins(allowedOrigins)
	apiHandler.SetSecureCookies(secureCookies)
	// So every participant (not just the host whose click triggered it) is
	// notified when a recording/RTMP stream starts or stops.
	apiHandler.SetRecordingBroadcaster(signalingHandler.BroadcastRecordingState)
	// So the host-only "remove participant" REST endpoint can actually
	// disconnect someone — apiHandler only owns RoomManager bookkeeping, not
	// the live WebSocket connections signalingHandler holds.
	apiHandler.SetParticipantRemover(signalingHandler.RemoveParticipant)
	// So the host-only "mute participant" REST endpoint (BR1) can actually
	// reach them.
	apiHandler.SetParticipantMuter(signalingHandler.ForceMuteParticipant)
	// TURN_SECRET empty (the default) leaves /api/v1/ice-servers returning
	// STUN-only — opt-in, not a hard requirement for a deployment that
	// hasn't set up the coturn service (docker-compose.yml's "turn"
	// profile). Without a TURN server, participants on restrictive/
	// symmetric NAT (common on mobile carriers and corporate networks)
	// silently can't connect at all — invisible on a single LAN, which is
	// exactly why this class of gap is easy to ship without noticing.
	apiHandler.SetTurnConfig(os.Getenv("TURN_SECRET"), os.Getenv("TURN_HOST"), os.Getenv("TURN_PORT"))

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

// resolveAllowedOrigins builds the final CORS/WS-upgrade origin allowlist:
// the operator-configured, comma-separated originsEnv (ALLOWED_ORIGINS),
// plus — automatically, not requiring separate configuration — the origin
// derived from frontendURL (FRONTEND_URL).
//
// The egress worker's headless Chromium is itself a WS client subject to
// this same origin allowlist: it connects using FRONTEND_URL's origin (the
// Docker-internal hostname, e.g. http://frontend:3000), which is never one
// of the browser-facing entries an operator would think to put in
// ALLOWED_ORIGINS. Without this, that connection is silently rejected —
// the egress recorder can never actually join the room, so every
// recording captures nothing at all, regardless of any client-side
// timing/readiness fix. FRONTEND_URL is already a trusted,
// operator-configured internal address, not user input, so folding its
// origin in here is safe.
//
// Pulled out of main() as a pure function (no env reads, no logging side
// effects beyond what's returned) specifically so this parsing/dedup logic
// — exactly the kind of thing that silently breaks egress for months, as
// it just did — has a direct unit test instead of only being checkable by
// booting the whole server.
func resolveAllowedOrigins(originsEnv, frontendURL string) []string {
	var allowedOrigins []string
	for _, o := range strings.Split(originsEnv, ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowedOrigins = append(allowedOrigins, o)
		}
	}

	if frontendURL == "" {
		return allowedOrigins
	}

	u, err := url.Parse(frontendURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		log.Printf("main: FRONTEND_URL %q could not be parsed as a URL — the egress worker's own WS connection may be rejected by ALLOWED_ORIGINS", frontendURL)
		return allowedOrigins
	}

	frontendOrigin := u.Scheme + "://" + u.Host
	for _, o := range allowedOrigins {
		if o == frontendOrigin {
			return allowedOrigins
		}
	}
	return append(allowedOrigins, frontendOrigin)
}
