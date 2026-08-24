import { test, expect, type Page } from '@playwright/test';

// Regression test for Issue 1 in the postmortem: ALLOWED_ORIGINS defaulted
// to exactly http://localhost:3000, silently rejecting any participant
// reaching the app via a different origin (127.0.0.1, a LAN IP, a phone) —
// their WS upgrade got a 403 before ever joining, with no visible error.
// .env.e2e now lists BOTH http://localhost:3000 and http://127.0.0.1:3000,
// so this proves a non-default-but-configured origin actually works end to
// end — not just that the Go unit test (isAllowedOrigin) is correct in
// isolation, but that the whole stack (browser Origin header -> WS upgrade
// -> AddParticipant -> track fan-out -> the other side's grid) agrees.
test('a participant joining from a different-but-allowed origin (127.0.0.1) appears in the host\'s grid', async ({ browser }) => {
  const roomSlug = `e2e-origin-${Date.now()}`;

  const hostContext = await browser.newContext();
  const guestContext = await browser.newContext();
  const hostPage: Page = await hostContext.newPage();
  const guestPage: Page = await guestContext.newPage();

  try {
    await hostPage.goto('http://localhost:3000/');
    await hostPage.getByRole('button', { name: /don't have an account\? sign up/i }).click();
    const suffix = `${Date.now()}-${Math.floor(Math.random() * 100000)}`;
    await hostPage.getByPlaceholder('e.g. Azhar (Instructor)').fill('E2E Origin Host');
    await hostPage.getByPlaceholder('you@example.com').fill(`e2e-origin-host-${suffix}@example.com`);
    await hostPage.getByPlaceholder('At least 8 characters').fill('correct-horse-battery-staple');
    await hostPage.getByRole('button', { name: /^sign up$/i }).click();
    await hostPage.getByPlaceholder('e.g. masterclass-golang').fill(roomSlug);
    await hostPage.getByRole('button', { name: /create & join as host/i }).click();
    await expect(hostPage.getByTestId('video-tile')).toHaveCount(1, { timeout: 15_000 });

    // Deliberately a DIFFERENT origin from the host's — same server, same
    // port, different hostname, so its browser-sent Origin header actually
    // differs (http://127.0.0.1:3000 vs http://localhost:3000).
    await guestPage.goto('http://127.0.0.1:3000/');
    await guestPage.getByPlaceholder('e.g. Budi (Developer)').fill('E2E Origin Guest');
    await guestPage.getByPlaceholder('e.g. demo-room').fill(roomSlug);
    await guestPage.getByRole('button', { name: /join room as guest/i }).click();

    // No "Couldn't connect — this address isn't allowed by the server"
    // banner (App.tsx's rejected-origin message) — the guest should reach
    // the meeting UI and see both themselves and the host. No intermediate
    // "exactly 1 tile" check here: the host is already publishing by the
    // time the guest joins, so the server's pre-existing-track push can
    // legitimately land before or after the guest's own self-tile render —
    // asserting exactly 1 would just race the app's own correct behavior
    // (see grid.spec.ts's comment on the same race).
    await expect(guestPage.getByTestId('video-tile')).toHaveCount(2, { timeout: 20_000 });

    // And, critically, the host — on the ORIGINAL origin — should see the
    // cross-origin guest show up in their grid too.
    await expect(hostPage.getByTestId('video-tile')).toHaveCount(2, { timeout: 20_000 });
  } finally {
    await hostContext.close();
    await guestContext.close();
  }
});
