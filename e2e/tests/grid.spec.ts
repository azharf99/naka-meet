import { test, expect, type Page } from '@playwright/test';

// L3: replaces the manual "open host on localhost, join as guest from
// another browser, eyeball whether a tile appears" testing this project has
// relied on until now. Two separate Playwright browser contexts stand in
// for two separate browsers/devices — real signaling, real SDP, real
// Redis-backed room state, real synthetic media via Chromium's fake device
// flags (see playwright.config.ts).

async function signUpAndCreateRoom(page: Page, roomSlug: string): Promise<void> {
  await page.goto('/');
  await page.getByRole('button', { name: /don't have an account\? sign up/i }).click();

  const suffix = `${Date.now()}-${Math.floor(Math.random() * 100000)}`;
  await page.getByPlaceholder('e.g. Azhar (Instructor)').fill('E2E Host');
  await page.getByPlaceholder('you@example.com').fill(`e2e-host-${suffix}@example.com`);
  await page.getByPlaceholder('At least 8 characters').fill('correct-horse-battery-staple');
  await page.getByRole('button', { name: /^sign up$/i }).click();

  // After signup, the same card switches to the "create room" form.
  await page.getByPlaceholder('e.g. masterclass-golang').fill(roomSlug);
  await page.getByRole('button', { name: /create & join as host/i }).click();
}

async function joinAsGuest(page: Page, name: string, roomSlug: string): Promise<void> {
  await page.goto('/');
  await page.getByPlaceholder('e.g. Budi (Developer)').fill(name);
  await page.getByPlaceholder('e.g. demo-room').fill(roomSlug);
  await page.getByRole('button', { name: /join room as guest/i }).click();
}

test('a guest joining from a separate browser context appears in the host\'s grid, and vice versa', async ({ browser }) => {
  const roomSlug = `e2e-grid-${Date.now()}`;

  const hostContext = await browser.newContext();
  const guestContext = await browser.newContext();
  const hostPage = await hostContext.newPage();
  const guestPage = await guestContext.newPage();

  try {
    await signUpAndCreateRoom(hostPage, roomSlug);
    // Host is alone in the room first — exactly one tile (their own).
    await expect(hostPage.getByTestId('video-tile')).toHaveCount(1, { timeout: 15_000 });

    await joinAsGuest(guestPage, 'E2E Guest', roomSlug);

    // The real assertion: once signaling/SDP/track fan-out has actually
    // completed, BOTH pages should show 2 tiles — this is what "grid
    // doesn't update for participants joining from a different browser"
    // (Issue 1) looked like when it was broken: one side stuck at 1. Note
    // there's no intermediate "guest sees exactly 1 tile" assertion here —
    // by the time the guest joins, the host is already publishing, so the
    // server's SubscribePeerToRoomTracks push of the host's pre-existing
    // track can legitimately land before the guest's own self-tile even
    // finishes its first render pass; asserting exactly 1 there would just
    // be racing the app's own (correct, fast) behavior.
    await expect(hostPage.getByTestId('video-tile')).toHaveCount(2, { timeout: 20_000 });
    await expect(guestPage.getByTestId('video-tile')).toHaveCount(2, { timeout: 20_000 });
  } finally {
    await hostContext.close();
    await guestContext.close();
  }
});
