package signaling

import "time"

// livenessAction is what a liveness tick decided should happen, if
// anything. The zero value (livenessNoop) means nothing changed.
type livenessAction int

const (
	livenessNoop livenessAction = iota
	// livenessBecameStale: publisher just crossed from healthy to stale —
	// broadcast peer_stale{stale:true} so every tile shows "reconnecting".
	livenessBecameStale
	// livenessRecovered: publisher was stale and RTP resumed before the
	// auto-remove deadline — broadcast peer_stale{stale:false}.
	livenessRecovered
	// livenessShouldRemove: publisher has been continuously stale past the
	// auto-remove threshold — the caller should force-remove them.
	livenessShouldRemove
)

// livenessState is the small piece of state a per-track watchdog carries
// between ticks: whether the track is currently considered stale, and since
// when (used to measure the auto-remove threshold independently of the
// stale-detection threshold).
type livenessState struct {
	stale      bool
	staleSince time.Time
}

// nextLivenessTick decides what a liveness watchdog should do on one poll,
// given how long it's been since the track's last RTP packet. Pulled out as
// a pure function (no goroutines, channels, or wall-clock reads of its own)
// specifically so the threshold/hysteresis/auto-remove logic — the part
// most likely to have an off-by-one or a flapping bug — has direct,
// deterministic unit tests instead of relying on real RTP timing, which
// TESTING_STRATEGY.md rules out for unit tests.
//
// silentFor is how long it's been since the last packet as of now.
// staleAfter is how long silence must persist before the track is
// considered stale. autoRemoveAfter is measured from staleSince, not from
// the start of the silence — a track that's flickered stale/recovered
// several times should get a fresh auto-remove clock each time it
// re-enters the stale state, not accumulate toward removal across
// separate, already-recovered stalls.
func nextLivenessTick(now time.Time, silentFor, staleAfter, autoRemoveAfter time.Duration, prev livenessState) (livenessState, livenessAction) {
	switch {
	case !prev.stale && silentFor >= staleAfter:
		return livenessState{stale: true, staleSince: now}, livenessBecameStale

	case prev.stale && silentFor < staleAfter:
		return livenessState{stale: false}, livenessRecovered

	case prev.stale && now.Sub(prev.staleSince) >= autoRemoveAfter:
		// Stay marked stale (the caller is about to remove the peer
		// entirely, but until that completes this track is still silent).
		return prev, livenessShouldRemove

	default:
		return prev, livenessNoop
	}
}
