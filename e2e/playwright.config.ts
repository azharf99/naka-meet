import { defineConfig, devices } from '@playwright/test';

// L3 multi-browser E2E (see TESTING_STRATEGY.md §5 and the postmortem this
// suite was written against): drives the frontend against a REAL, locally
// built stack — see docker-compose.e2e.yml — with 2-3 separate browser
// contexts standing in for "different browsers/devices", the same shape as
// the origin-mismatch bug (Issue 1) that manual single-laptop testing
// couldn't catch. Assumes the stack is already up (see the README in this
// directory / CI's test-e2e job) — this config does not start it itself,
// since the same running stack is reused across every test file rather than
// torn down and rebuilt per file.
export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 60_000,
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:3000',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
    // Real camera/mic access is never available (or desired) in CI/headless
    // — Chromium's fake device flags supply synthetic media so
    // getUserMedia() resolves with real tracks instead of hanging on an
    // unanswerable permission prompt or a real (absent) device.
    launchOptions: {
      args: [
        '--use-fake-device-for-media-stream',
        '--use-fake-ui-for-media-stream',
      ],
    },
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
