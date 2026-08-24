import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { readdirSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import Redis from 'ioredis';

// e2e/package.json declares "type": "module", so __dirname (a CommonJS
// global) isn't available here — derive it from import.meta.url instead.
const __dirname = dirname(fileURLToPath(import.meta.url));

// L4 (see TESTING_STRATEGY.md §5 and the postmortem's testing pyramid):
// trigger a real recording against the real stack, let it run a few
// seconds, stop it, and assert with ffprobe that the output is a real,
// non-trivial video file — not just that the API call returned 200. This
// is the cheap check that would have caught Issue 3 (recordings coming
// back empty/incomplete): nothing before this suite ever inspected the
// actual output file, only whether the trigger endpoint accepted the
// request.
//
// Single host is enough here — the egress recorder joins the same room
// and receives the host's own published (fake) camera track through the
// normal SFU fan-out, same as any other participant would.

const RECORDINGS_DIR = join(__dirname, '..', '..', 'recordings');

test('a triggered recording produces a real video file with a video stream and non-trivial duration', async ({ page }) => {
  test.setTimeout(120_000);

  const roomSlug = `e2e-recording-${Date.now()}`;

  // Subscribe before triggering anything, so a fast completion can't race
  // past a subscription that hasn't started yet.
  const sub = new Redis('redis://localhost:6379');
  const completed = new Promise<void>((resolve) => {
    sub.on('message', (_channel, message) => {
      try {
        const parsed = JSON.parse(message);
        if (parsed.status === 'completed' && parsed.room === roomSlug) resolve();
      } catch {
        // Not JSON, or not the shape we expect — ignore.
      }
    });
  });
  await sub.subscribe('channel:egress_status');

  try {
    await page.goto('/');
    await page.getByRole('button', { name: /don't have an account\? sign up/i }).click();
    const suffix = `${Date.now()}-${Math.floor(Math.random() * 100000)}`;
    await page.getByPlaceholder('e.g. Azhar (Instructor)').fill('E2E Recording Host');
    await page.getByPlaceholder('you@example.com').fill(`e2e-recording-host-${suffix}@example.com`);
    await page.getByPlaceholder('At least 8 characters').fill('correct-horse-battery-staple');
    await page.getByRole('button', { name: /^sign up$/i }).click();
    await page.getByPlaceholder('e.g. masterclass-golang').fill(roomSlug);
    await page.getByRole('button', { name: /create & join as host/i }).click();

    // Own tile up — fake camera track is actually flowing before we ask
    // the egress worker to capture anything.
    await expect(page.getByTestId('video-tile')).toHaveCount(1, { timeout: 15_000 });

    const recordButton = page.getByTitle('Record Room to Persistent Storage');
    await recordButton.click();
    await expect(recordButton).toHaveText(/REC Active/, { timeout: 10_000 });

    // Let it actually capture something — the egress worker's own
    // readiness-gate + Puppeteer/Xvfb/ffmpeg startup isn't instantaneous,
    // and a 0-second clip isn't a meaningful "non-trivial duration" check.
    await page.waitForTimeout(15_000);

    await recordButton.click();
    await expect(recordButton).toHaveText(/^Record$/, { timeout: 10_000 });

    // BR2/worker.js always signals SIGINT (never SIGKILL) so the MP4
    // container's metadata header is finalized correctly — wait for the
    // worker's own "completed" signal rather than guessing a fixed delay
    // for however long that finalization takes.
    await Promise.race([
      completed,
      new Promise((_, reject) => setTimeout(() => reject(new Error('timed out waiting for channel:egress_status "completed"')), 30_000)),
    ]);

    const matchingFiles = readdirSync(RECORDINGS_DIR).filter((f) => f.startsWith(`${roomSlug}_`) && f.endsWith('.mp4'));
    expect(matchingFiles.length, `expected a recording file for ${roomSlug} in ${RECORDINGS_DIR}, found: ${readdirSync(RECORDINGS_DIR).join(', ')}`).toBeGreaterThan(0);

    const filePath = join(RECORDINGS_DIR, matchingFiles[0]);
    const fileSize = statSync(filePath).size;
    // A noise-floor sanity check before even trusting ffprobe on it — an
    // empty/near-empty file is exactly the Issue 3 symptom this test
    // exists to catch.
    expect(fileSize, `recording file ${filePath} is suspiciously small (${fileSize} bytes) — looks like an empty/failed capture`).toBeGreaterThan(10_000);

    const probeOutput = execFileSync(
      'ffprobe',
      ['-v', 'error', '-show_entries', 'format=duration:stream=codec_type', '-of', 'json', filePath],
      { encoding: 'utf-8' }
    );
    const probe = JSON.parse(probeOutput);

    const duration = parseFloat(probe.format?.duration ?? '0');
    expect(duration, `recording duration (${duration}s) should reflect the ~15s capture window, not a near-zero/empty clip`).toBeGreaterThan(3);

    const hasVideoStream = (probe.streams ?? []).some((s: { codec_type: string }) => s.codec_type === 'video');
    expect(hasVideoStream, 'recording should contain a video stream, not just audio/nothing').toBe(true);
  } finally {
    await sub.quit().catch(() => {});
  }
});
