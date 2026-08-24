# L3 E2E: multi-browser tests against the real stack

Per `TESTING_STRATEGY.md` §5 and the postmortem's testing pyramid: this
replaces the manual "open host on localhost, join as guest from another
browser, eyeball whether a tile appears" testing this project has relied on.
Two (or more) separate Playwright browser contexts stand in for separate
browsers/devices, driving the actual UI (signup, room creation, guest join)
against a fully containerized stack — real signaling, real Redis, real
Postgres, real SDP negotiation.

## Running locally

1. Build and start the stack using the E2E override (builds fresh images
   from source instead of pulling `:latest`, and uses `.env.e2e` — a
   throwaway, non-secret env file, safe to commit — instead of your real
   `.env`). An isolated Compose project name is used so it can't collide
   with a normal dev stack's containers/volumes if you have one running.

   ```bash
   docker-compose -p naka-meet-e2e -f docker-compose.yml -f docker-compose.e2e.yml up --build -d
   ```

   Note: `docker-compose.yml`'s services use fixed `container_name`s, so
   this E2E stack still can't run *at the same time* as a normal
   `docker-compose up` dev stack — stop one before starting the other.

2. Install dependencies and Playwright's browser once:

   ```bash
   cd e2e
   npm install
   npx playwright install --with-deps chromium
   ```

3. Run the suite:

   ```bash
   npm test
   ```

4. Tear down (including the Postgres volume, so the next run starts from a
   clean database rather than reusing stale credentials/data):

   ```bash
   docker-compose -p naka-meet-e2e -f docker-compose.yml -f docker-compose.e2e.yml down -v
   ```

## A known limitation on Windows + Docker Desktop

Verifying this suite while building it, running Playwright natively on
Windows against a Docker Desktop stack, the single-peer legs all verified
cleanly end to end — signup, DB-backed room creation, guest join, each
side's own initial SDP offer/answer completing, each side rendering its own
self-tile. The cross-peer leg (each side actually seeing the *other's*
track) could not be confirmed from that environment: ICE gathering
succeeded on both sides (host and server-reflexive candidates were
exchanged over the signaling WS), but no actual RTP ever appeared to
reach the SFU, so the server-side `pc.OnTrack` that triggers the
`track_metadata`/renegotiation broadcast never fired.

This matches Docker Desktop's WSL2-VM network boundary — a plain TCP probe
from the Windows host to the SFU container's internal bridge IP
(`172.18.0.x`) failed outright, and the srflx candidates gathered are also
subject to same-machine NAT hairpin behavior that varies by router/firewall
and isn't representative of two independent participants. Neither of these
apply to the target CI environment (GitHub Actions `ubuntu-latest`, Docker
running natively — no VM boundary between the runner's own network stack
and the docker0 bridge) or to a real deployment (the project is documented
as developed/tested on Windows + **WSL2** + Docker, not Docker Desktop's
Windows-native networking path).

If `test-e2e` in CI shows the same cross-peer symptom (tile count stuck at
1 on both sides) rather than this being purely a local artifact, that would
point at a genuine gap in the SFU's ICE candidate advertisement when
running inside Docker (e.g. needing `SettingEngine.SetNAT1To1IPs` so it
advertises a reachable address instead of the container-internal one) —
worth a follow-up investigation, not something this suite's job is to fix.
