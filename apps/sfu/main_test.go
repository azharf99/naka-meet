package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveAllowedOrigins_IncludesFrontendURLOrigin is the regression
// test for a real bug found manually testing egress recording on staging:
// the egress worker's headless Chromium connects to the signaling
// WebSocket using FRONTEND_URL's origin (e.g. http://frontend:3000), which
// is never one of the browser-facing ALLOWED_ORIGINS entries an operator
// would think to configure — so every recording silently captured
// nothing, regardless of any client-side timing fix, because the egress
// recorder could never even join the room.
func TestResolveAllowedOrigins_IncludesFrontendURLOrigin(t *testing.T) {
	origins := resolveAllowedOrigins("http://localhost:3000", "http://frontend:3000")
	assert.Contains(t, origins, "http://localhost:3000")
	assert.Contains(t, origins, "http://frontend:3000")
}

func TestResolveAllowedOrigins_DoesNotDuplicateAnAlreadyListedFrontendOrigin(t *testing.T) {
	origins := resolveAllowedOrigins("http://localhost:3000,http://frontend:3000", "http://frontend:3000")
	count := 0
	for _, o := range origins {
		if o == "http://frontend:3000" {
			count++
		}
	}
	assert.Equal(t, 1, count, "an operator who already listed the frontend origin explicitly should not get it twice")
}

func TestResolveAllowedOrigins_StripsPathFromFrontendURL(t *testing.T) {
	// FRONTEND_URL is documented/used as a bare origin, but be defensive:
	// only the scheme+host should end up in the allowlist, matching what a
	// browser actually sends as its Origin header (no path).
	origins := resolveAllowedOrigins("http://localhost:3000", "http://frontend:3000/some/path")
	assert.Contains(t, origins, "http://frontend:3000")
	assert.NotContains(t, origins, "http://frontend:3000/some/path")
}

func TestResolveAllowedOrigins_EmptyFrontendURLLeavesListUnchanged(t *testing.T) {
	origins := resolveAllowedOrigins("http://localhost:3000", "")
	assert.Equal(t, []string{"http://localhost:3000"}, origins)
}

func TestResolveAllowedOrigins_UnparsableFrontendURLDegradesGracefully(t *testing.T) {
	// A malformed FRONTEND_URL must not crash startup or corrupt the
	// existing allowlist — just leave it as-is and let the (already
	// logged) misconfiguration surface as connection failures instead.
	origins := resolveAllowedOrigins("http://localhost:3000", "://not-a-url")
	assert.Equal(t, []string{"http://localhost:3000"}, origins)
}

func TestResolveAllowedOrigins_TrimsAndSkipsEmptyEntriesInOriginsEnv(t *testing.T) {
	origins := resolveAllowedOrigins(" http://localhost:3000 ,,http://127.0.0.1:3000 ", "")
	assert.Equal(t, []string{"http://localhost:3000", "http://127.0.0.1:3000"}, origins)
}
