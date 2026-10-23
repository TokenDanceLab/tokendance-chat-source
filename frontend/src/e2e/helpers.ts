import { expect, type Page } from "@playwright/test";
import { STORAGE_KEYS } from "../lib/storage";

export async function waitForLightChatReady(page: Page) {
  await expect(page.getByRole("log", { name: /^(公共聊天|Public Chat)$/ })).toBeVisible({ timeout: 15000 });
  const composer = page.locator("[data-visual='composer-textarea']");
  await expect(composer).toBeVisible({ timeout: 15000 });
  await expect(composer).toBeEnabled({ timeout: 15000 });
  return composer;
}

/**
 * Joins as a guest. Guest handles are bound to the browser's HttpOnly guest
 * cookie, so a fresh identity means a fresh cookie: pass fresh=false to rejoin
 * as the same browser (a second tab, or after closing the page).
 */
export async function joinGuestFromPreview(
  page: Page,
  username: string,
  { fresh = true }: { fresh?: boolean } = {},
) {
  await page.evaluate(
    ({ name, keys }) => {
      localStorage.setItem(keys.guestName, name);
      sessionStorage.removeItem(keys.guestName);
      localStorage.removeItem(keys.username);
      localStorage.removeItem(keys.auth);
    },
    { name: username, keys: STORAGE_KEYS },
  );
  if (fresh) await page.context().clearCookies();
  await page.reload();
  return waitForLightChatReady(page);
}

export async function openAccountMenu(page: Page) {
  const sidebar = page.locator("[data-visual='light-chat-sidebar']:visible");
  await sidebar.getByRole("button", { name: /^(账户菜单|Account menu)$/ }).click();
}

export async function openAccountSettings(page: Page) {
  await openAccountMenu(page);
  await page
    .getByRole("menuitem", { name: /^(打开设置|Open Settings)$/ })
    .click();
}

export async function leaveChat(page: Page) {
  await openAccountMenu(page);
  await page.getByRole("menuitem", { name: /^(离开聊天|Leave chat)$/ }).click();
}
