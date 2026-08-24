package signaling

// This file is package signaling (not signaling_test, unlike every other
// test file here) deliberately: nextLivenessTick is pure, unexported logic
// with no goroutines/channels/network, and testing it as a white-box unit
// keeps the threshold/hysteresis/auto-remove decisions — the part most
// likely to hide an off-by-one or a flapping bug — covered without needing
// real RTP timing, which TESTING_STRATEGY.md rules out for unit tests. The
// goroutine that actually drives this from real packet timestamps is thin
// glue tested at the integration level instead (see handler_test.go's
// TestSignaling_RemoveParticipant_* tests for the removal mechanics it
// calls into).

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNextLivenessTick_StaysHealthyBeforeThreshold(t *testing.T) {
	now := time.Now()
	next, action := nextLivenessTick(now, 3*time.Second, 8*time.Second, 45*time.Second, livenessState{})
	assert.Equal(t, livenessNoop, action)
	assert.False(t, next.stale)
}

func TestNextLivenessTick_BecomesStaleAtThreshold(t *testing.T) {
	now := time.Now()
	next, action := nextLivenessTick(now, 8*time.Second, 8*time.Second, 45*time.Second, livenessState{})
	assert.Equal(t, livenessBecameStale, action)
	assert.True(t, next.stale)
	assert.Equal(t, now, next.staleSince)
}

func TestNextLivenessTick_StaysStaleBeforeAutoRemove(t *testing.T) {
	staleSince := time.Now().Add(-10 * time.Second)
	prev := livenessState{stale: true, staleSince: staleSince}
	next, action := nextLivenessTick(time.Now(), 10*time.Second, 8*time.Second, 45*time.Second, prev)
	assert.Equal(t, livenessNoop, action)
	assert.True(t, next.stale)
	assert.Equal(t, staleSince, next.staleSince, "staleSince must not reset while still silently stale")
}

func TestNextLivenessTick_RecoversWhenPacketsResume(t *testing.T) {
	prev := livenessState{stale: true, staleSince: time.Now().Add(-5 * time.Second)}
	// silentFor < staleAfter means a packet has arrived recently enough
	// that this track is no longer silent.
	next, action := nextLivenessTick(time.Now(), 1*time.Second, 8*time.Second, 45*time.Second, prev)
	assert.Equal(t, livenessRecovered, action)
	assert.False(t, next.stale)
}

func TestNextLivenessTick_SignalsRemoveAfterAutoRemoveThreshold(t *testing.T) {
	staleSince := time.Now().Add(-45 * time.Second)
	prev := livenessState{stale: true, staleSince: staleSince}
	next, action := nextLivenessTick(time.Now(), 45*time.Second, 8*time.Second, 45*time.Second, prev)
	assert.Equal(t, livenessShouldRemove, action)
	assert.True(t, next.stale)
}

func TestNextLivenessTick_AutoRemoveClockRestartsAfterARecovery(t *testing.T) {
	// A track that recovered and went stale again must get a fresh
	// auto-remove clock from the new stale episode, not the old one —
	// otherwise a peer with several short, already-recovered stalls could
	// get removed based on total accumulated silence across unrelated
	// episodes instead of one continuous stall.
	recoveredState := livenessState{stale: false}
	staleAgain, action := nextLivenessTick(time.Now(), 8*time.Second, 8*time.Second, 45*time.Second, recoveredState)
	assert.Equal(t, livenessBecameStale, action)

	// Immediately after re-entering stale, even though *some* wall-clock
	// time has passed since the very first stall began, the fresh
	// staleSince means auto-remove must not fire yet.
	next, action := nextLivenessTick(staleAgain.staleSince.Add(1*time.Second), 8*time.Second, 8*time.Second, 45*time.Second, staleAgain)
	assert.Equal(t, livenessNoop, action)
	assert.True(t, next.stale)
}
