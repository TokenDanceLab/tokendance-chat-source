import { expect, test } from "@playwright/test";

test.use({ serviceWorkers: "block" });

const accountName = "account_logout_e2e";

test.describe("Account logout boundary", () => {
  for (const mobile of [false, true]) {
    test(`keeps Chat and Account logout distinct on ${mobile ? "mobile" : "desktop"}`, async ({
      page,
    }) => {
      if (mobile) await page.setViewportSize({ width: 390, height: 844 });
      await page.addInitScript(() => {
        localStorage.setItem("tokendance:lang", "zh-CN");
        localStorage.setItem("tdchat-theme", "light");
      });

      let authenticated = true;
      await page.route("**/api/session", (route) =>
        route.fulfill({
          json: authenticated
            ? {
                authenticated: true,
                username: accountName,
                kind: "oidc",
                csrf_token: "account-e2e-csrf",
              }
            : { authenticated: false },
        }),
      );
      await page.route("**/api/config", (route) =>
        route.fulfill({
          json: {
            bot_name: "TokenBot",
            model: "test-model",
            llm_enabled: false,
            oidc_enabled: true,
            local_auth_enabled: false,
            max_message_length: 2000,
          },
        }),
      );
      await page.route("**/api/logout/account", async (route) => {
        expect(route.request().method()).toBe("POST");
        expect(route.request().headers()["x-csrf-token"]).toBe("account-e2e-csrf");
        authenticated = false;
        await route.fulfill({
          status: 200,
          json: { redirect_url: "/api/logout/account/callback?state=account-e2e-state" },
        });
      });
      await page.route("**/api/logout/account/callback?state=account-e2e-state", (route) =>
        route.fulfill({
          status: 303,
          headers: { Location: "/?oidc_logged_out=1" },
        }),
      );

      await page.goto("/");
      await expect(page.locator("[data-visual='composer-textarea']")).toBeEnabled({
        timeout: 15000,
      });

      if (mobile) await page.getByLabel(/打开侧边栏|Open sidebar/).click();
      const sidebar = page.locator("[data-visual='light-chat-sidebar']:visible");
      await sidebar.getByRole("button", { name: /账户菜单|Account menu/ }).click();

      await expect(page.getByRole("menuitem", { name: "退出 TokenDance Account" })).toBeVisible();
      await expect(page.getByRole("menuitem", { name: "离开聊天" })).toBeVisible();
      await page.screenshot({
        path: test.info().outputPath(`account-menu-${mobile ? "mobile" : "desktop"}.png`),
        fullPage: true,
        animations: "disabled",
      });

      await page.getByRole("menuitem", { name: "退出 TokenDance Account" }).click();
      await expect(page.getByText("已退出 TokenDance Account")).toBeVisible({ timeout: 15000 });
      await expect(page).toHaveURL(/\/$/);
      expect(authenticated).toBe(false);
    });
  }
});
