import { randomUUID } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import { joinGuestFromPreview, openAccountSettings } from "./helpers";

// These cases use the real Chat backend and a separate loopback model fixture.
// The fixture returns 500 once for retry-*, delays delay-*, and streams slow-*.
const fixture = process.env.E2E_CHAT_FIXTURE_URL;
test.skip(!fixture, "Requires the disposable Chat/model integration fixture");
test.use({ serviceWorkers: "block", locale: "en-US", viewport: { width: 1440, height: 900 } });

declare global {
  interface Window {
    chatNotificationFixture: { hidden: boolean; notifications: Array<{ title: string; body?: string }> };
  }
}

test.beforeAll(({ baseURL }) => {
  for (const address of [baseURL, fixture]) {
    expect(["127.0.0.1", "localhost", "[::1]"]).toContain(new URL(address!).hostname);
  }
});

async function join(page: Page, name = `ui_${randomUUID().slice(0, 8)}`, language: "en-US" | null = "en-US") {
  await page.addInitScript((lang) => {
    if (lang) localStorage.setItem("tokendance:lang", lang);
    else localStorage.removeItem("tokendance:lang");
    localStorage.setItem("tdchat-theme", "light");
  }, language);
  await page.goto("/");
  await joinGuestFromPreview(page, name);
  return name;
}

async function selectSpace(page: Page, privateChat: boolean) {
  const open = page.getByRole("button", { name: "Open sidebar", exact: true });
  if (await open.isVisible()) await open.click();
  await page.locator('[data-visual="light-chat-sidebar"]:visible').getByRole("button", {
    name: privateChat ? /TokenBot/ : "Public Chat", exact: !privateChat,
  }).click();
}

async function send(page: Page, text: string) {
  const input = page.locator('[data-visual="composer-textarea"]');
  await expect(input).toBeEnabled();
  await input.fill(text);
  await input.press("Enter");
}

async function notificationFixture(page: Page) {
  await page.addInitScript(() => {
    window.chatNotificationFixture = { hidden: false, notifications: [] };
    Object.defineProperty(document, "hidden", { configurable: true, get: () => window.chatNotificationFixture.hidden });
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => window.chatNotificationFixture.hidden ? "hidden" : "visible" });
    class FixtureNotification {
      static permission = "granted";
      constructor(title: string, options: NotificationOptions = {}) {
        window.chatNotificationFixture.notifications.push({ title, body: options.body });
      }
      close() {}
    }
    Object.defineProperty(window, "Notification", { configurable: true, value: FixtureNotification });
  });
}

async function background(page: Page, hidden: boolean) {
  await page.evaluate((value) => {
    window.chatNotificationFixture.hidden = value;
    document.dispatchEvent(new Event("visibilitychange"));
  }, hidden);
}

test("a real upstream 500 can be retried from the failed private run", async ({ page, request }) => {
  const key = `retry-${randomUUID()}`;
  await join(page);
  await selectSpace(page, true);
  await send(page, `[fixture:${key}]`);
  const retry = page.locator('[data-visual="run-retry"]');
  await expect(retry).toBeVisible();
  await retry.click();
  await expect(page.getByText(`Fixture reply: ${key}`, { exact: true })).toBeVisible();
  const stats = await (await request.get(`${fixture}/stats`)).json();
  expect(stats[key]).toEqual({ requests: 2, failures: 1, successes: 1 });
  await page.screenshot({ path: test.info().outputPath("private-retry.png"), animations: "disabled" });
});

test("stop retains the partial private answer and offers retry", async ({ page }) => {
  const key = `slow-${randomUUID()}`;
  await join(page);
  await selectSpace(page, true);
  await send(page, `[fixture:${key}]`);
  const partial = page.locator('[data-visual="message-bubble-surface"]').filter({ hasText: "Fixture partial" });
  await expect(partial).toBeVisible();
  await page.locator('[data-visual="composer-stop"]').click();
  await expect(page.locator('[data-visual="message-stopped"]')).toBeVisible();
  await expect(partial).toBeVisible();
  await expect(page.locator('[data-visual="message-stopped-retry"]')).toBeVisible();
  await expect(page.locator('[data-visual="composer-stop"]')).toHaveCount(0);
  await page.screenshot({ path: test.info().outputPath("private-stopped.png"), animations: "disabled" });
});

test("private replies create and clear one unread badge without reaching another guest", async ({ page, browser, baseURL }) => {
  const peerContext = await browser.newContext({ baseURL, serviceWorkers: "block" });
  try {
    const peer = await peerContext.newPage();
    await join(page);
    await join(peer);
    await selectSpace(peer, true);
    await selectSpace(page, true);
    const key = `delay-${randomUUID()}`;
    await send(page, `[fixture:${key}]`);
    await selectSpace(page, false);
    const badge = page.locator('[data-visual="sidebar-unread"]');
    await expect(badge).toHaveText("1");
    await expect(peer.getByText(`Fixture reply: ${key}`, { exact: true })).toHaveCount(0);
    await expect(peer.locator('[data-visual="sidebar-unread"]')).toHaveCount(0);
    await selectSpace(page, true);
    await expect(badge).toHaveCount(0);
    await expect(page.getByText(`Fixture reply: ${key}`, { exact: true })).toBeVisible();
  } finally {
    await peerContext.close();
  }
});

test("background private replies update title and badge with a content-free notification", async ({ page }) => {
  await notificationFixture(page);
  await join(page, undefined, null);
  await selectSpace(page, true);
  const key = `delay-${randomUUID()}`;
  await send(page, `[fixture:${key}]`);
  await background(page, true);
  await expect(page).toHaveTitle(/^\(1\)/);
  await expect(page.locator('[data-visual="sidebar-unread"]')).toHaveText("1");
  expect(await page.evaluate(() => window.chatNotificationFixture.notifications)).toEqual([
    { title: "TokenBot", body: "New reply from TokenBot" },
  ]);
  await background(page, false);
  await expect(page).toHaveTitle("TokenDance Chat");
  await expect(page.locator('[data-visual="sidebar-unread"]')).toHaveCount(0);
});

test("the private notification switch persists and suppresses alerts without hiding unread replies", async ({ page }) => {
  await notificationFixture(page);
  await join(page);
  await openAccountSettings(page);
  const toggle = page.getByRole("switch", { name: "TokenBot Notifications", exact: true });
  await expect(toggle).toBeChecked();
  await toggle.click();
  await expect(toggle).not.toBeChecked();
  await page.reload();
  await expect(page.locator('[data-visual="composer-textarea"]')).toBeEnabled();
  await openAccountSettings(page);
  await expect(toggle).not.toBeChecked();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await selectSpace(page, true);
  await send(page, `[fixture:delay-${randomUUID()}]`);
  await background(page, true);
  await expect(page.locator('[data-visual="sidebar-unread"]')).toHaveText("1");
  expect(await page.evaluate(() => window.chatNotificationFixture.notifications)).toEqual([]);
  await expect(page).toHaveTitle("TokenDance Chat");
  await background(page, false);
  await openAccountSettings(page);
  await toggle.click();
  await expect(toggle).toBeChecked();
  await page.screenshot({ path: test.info().outputPath("private-notifications.png"), animations: "disabled" });
});

test("blocking requires two clicks and settings can restore message delivery", async ({ page, browser, baseURL }) => {
  const peerContext = await browser.newContext({ baseURL, serviceWorkers: "block" });
  try {
    const peer = await peerContext.newPage();
    await join(page);
    await join(peer);
    const first = `Before block ${randomUUID()}`;
    await send(peer, first);
    const row = page.locator('[data-visual="message-bubble"]').filter({ hasText: first });
    await expect(row).toBeVisible();
    await row.hover();
    await row.locator('[data-visual="message-bubble-menu"]:visible').click();
    const block = page.getByRole("menuitem", { name: /^Block @/ });
    const label = (await block.innerText()).trim();
    await block.click();
    const confirm = page.getByRole("menuitem", { name: label.replace(/^Block /, "Click again to block "), exact: true });
    await expect(confirm).toBeVisible();
    await confirm.click();
    const blocked = `Blocked message ${randomUUID()}`;
    await send(peer, blocked);
    await expect(peer.getByText(blocked, { exact: true })).toBeVisible();
    await expect(page.getByText(blocked, { exact: true })).toHaveCount(0);
    await openAccountSettings(page);
    await page.getByRole("button", { name: "Unblock", exact: true }).click();
    await expect(page.getByText("Blocked users", { exact: true })).toHaveCount(0);
    await page.getByRole("button", { name: "Close", exact: true }).click();
    const restored = `After unblock ${randomUUID()}`;
    await send(peer, restored);
    await expect(page.getByText(restored, { exact: true })).toBeVisible();
  } finally {
    await peerContext.close();
  }
});

test("mobile long press exposes usable actions and a short viewport keeps the draft visible", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await join(page);
  const text = `Long press ${randomUUID()}`;
  await send(page, text);
  const surface = page.locator('[data-visual="message-bubble-surface"]').filter({ hasText: text });
  await expect(surface).toBeVisible();
  const box = await surface.boundingBox();
  expect(box).not.toBeNull();
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: true });
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 }] });
  const menu = page.getByRole("menu", { name: "Message actions", exact: true });
  await expect(menu).toBeVisible();
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await expect(menu).toBeVisible();
  const actions = await menu.getByRole("menuitem").all();
  expect(actions.length).toBeGreaterThanOrEqual(3);
  for (const item of actions) {
    expect((await item.boundingBox())!.height).toBeGreaterThanOrEqual(43.9);
  }
  await page.screenshot({ path: test.info().outputPath("mobile-long-press.png"), animations: "disabled" });
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  const input = page.locator('[data-visual="composer-textarea"]');
  await input.fill("Unsent local draft");
  await page.setViewportSize({ width: 390, height: 460 });
  await expect(input).toHaveValue("Unsent local draft");
  await expect.poll(() => page.locator('[data-visual="composer-body"]').evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return rect.top >= 0 && rect.bottom <= innerHeight;
  })).toBe(true);
  await page.screenshot({ path: test.info().outputPath("short-viewport-composer.png"), animations: "disabled" });
});

test.describe("real service worker update", () => {
  test.use({ serviceWorkers: "allow" });

  test("first install stays quiet and an updated worker shows a working refresh action", async ({ page, request }) => {
    await join(page);
    await page.evaluate(() => navigator.serviceWorker.ready.then(() => undefined));
    await expect(page.getByText("A new version is ready", { exact: true })).toHaveCount(0);
    await page.reload();
    await expect(page.locator('[data-visual="composer-textarea"]')).toBeEnabled();
    const rotated = await request.post(`${fixture}/rotate-worker`);
    expect(rotated.ok()).toBe(true);
    await page.evaluate(async () => { await (await navigator.serviceWorker.getRegistration())?.update(); });
    await expect(page.getByText("A new version is ready", { exact: true })).toBeVisible();
    await page.screenshot({ path: test.info().outputPath("worker-update.png"), animations: "disabled" });
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(page.locator('[data-visual="composer-textarea"]')).toBeEnabled();
    await expect(page.getByText("A new version is ready", { exact: true })).toHaveCount(0);
  });
});
