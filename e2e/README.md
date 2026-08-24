# L3/L4 E2E: multi-browser + recording tests against the real stack

Per `TESTING_STRATEGY.md` §5 and the postmortem's testing pyramid.

**L3** (`grid.spec.ts`, `origin.spec.ts`) replaces the manual "open host on
localhost, join as guest from another browser, eyeball whether a tile
appears" testing this project has relied on. Two (or more) separate
Playwright browser contexts stand in for separate browsers/devices, driving
the actual UI (signup, room creation, guest join) against a fully
containerized stack — real signaling, real Redis, real Postgres, real SDP
negotiation.

**L4** (`recording.spec.ts`) builds on the same harness: a host triggers a
real recording via the actual Controls UI, lets it run for a real ~15s
window, stops it, and asserts with `ffprobe` — not just "the API call
returned 200" — that the output is a real, non-trivial video file (file
size above a noise floor, `ffprobe`-reported duration reflecting the actual
capture window, an actual video stream present). This is the cheap check
that would have caught Issue 3 (recordings coming back empty/incomplete):
nothing before this suite ever inspected the produced file.

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

   `recording.spec.ts` (L4) additionally needs `ffprobe` on `PATH` (part of
   any `ffmpeg` install) to inspect the produced recording.

3. Run the suite:

   ```bash
   npm test
   ```

4. Tear down (including the Postgres volume, so the next run starts from a
   clean database rather than reusing stale credentials/data):

   ```bash
   docker-compose -p naka-meet-e2e -f docker-compose.yml -f docker-compose.e2e.yml down -v
   ```

## A known limitation on Windows + Docker Desktop (confirmed local-only)

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

That traced to Docker Desktop's WSL2-VM network boundary — a plain TCP
probe from the Windows host to the SFU container's internal bridge IP
(`172.18.0.x`) failed outright — plus same-machine NAT-hairpin behavior for
the gathered srflx candidates, neither of which apply on a real Linux
Docker host. **Confirmed on CI** (GitHub Actions `ubuntu-latest`, native
Docker): the cross-peer leg works correctly — both `grid.spec.ts` and
`origin.spec.ts` pass, including the guest seeing the host's pre-existing
track land essentially immediately after joining (fast enough that an
early version of these tests, which asserted the guest saw exactly one
tile right after joining, flaked against the app's own correct, fast
behavior — see git history). So: genuinely a local-Windows-only artifact,
not a gap in the SFU's ICE handling. `test-e2e` is part of
`build-and-deploy`'s required checks.

If you're verifying changes locally on Windows + Docker Desktop, expect
the cross-peer assertions specifically to fail or hang for this same
reason — the single-peer setup steps (signup, room creation, guest join)
are still meaningful to check locally, the final tile-count assertions are
not. `recording.spec.ts` (L4) is affected the same way for the same root
cause: the egress recorder's own container-to-container leg to the SFU is
fine, but the host browser's external-to-container leg (the one actually
publishing the camera track being recorded) is exactly the leg that
doesn't work locally, so there's nothing real for the egress worker to
capture either. Not independently confirmed on CI at the time this test
was written (unlike L3, which was) — if it doesn't pass there either,
treat that as a real finding, not another instance of this same local-only
artifact.
