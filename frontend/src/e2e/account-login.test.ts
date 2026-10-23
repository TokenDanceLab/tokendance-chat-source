import { expect, test, type Locator } from "@playwright/test";

test.use({ serviceWorkers: "block", contextOptions: { reducedMotion: "reduce" } });

// DOM visibility alone does not catch foreground/background becoming identical.
// Canvas resolves CSS colors (including OKLCH) to sRGB for WCAG contrast.
async function contrastRatio(link: Locator): Promise<number> {
  return link.evaluate((element) => {
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 1;
    const context = canvas.getContext("2d")!;
    const luminance = (color: string) => {
      context.clearRect(0, 0, 1, 1);
      context.fillStyle = color;
      context.fillRect(0, 0, 1, 1);
      const channels = context.getImageData(0, 0, 1, 1).data;
      const linear = Array.from(channels).slice(0, 3).map((channel) => {
        const value = channel / 255;
        return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
      });
      return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722;
    };
    const style = getComputedStyle(element);
    const foreground = luminance(style.color);
    const background = luminance(style.backgroundColor);
    return (Math.max(foreground, background) + 0.05) /
      (Math.min(foreground, background) + 0.05);
  });
}

test.describe("Account login CTA", () => {
  for (const width of [1440, 390, 320]) {
    const mobile = width < 768;
    for (const theme of ["light", "dark"]) {
      for (const language of ["zh-CN", "en"]) {
        test(`${mobile ? "mobile" : "desktop"} ${width} ${theme} ${language} keeps the login label readable`, async ({ page }) => {
          await page.setViewportSize(mobile
            ? { width, height: 844 }
            : { width, height: 900 });
          await page.addInitScript(({ theme, language }) => {
            localStorage.setItem("tokendance:lang", language);
            localStorage.setItem("tdchat-theme", theme);
          }, { theme, language });
          await page.route("**/api/session", (route) => route.fulfill({
            json: { authenticated: false },
          }));
          await page.route("**/api/config", (route) => route.fulfill({
            json: {
              bot_name: "TokenBot",
              llm_enabled: false,
              oidc_enabled: true,
              local_auth_enabled: false,
              max_message_length: 2000,
            },
          }));

          await page.goto("/");
          if (mobile) await page.getByLabel(/打开侧边栏|Open sidebar/).click();
          // Brand colors stay canonical in dark mode; CSS inversion turns
          // the blue family mark orange even while the CTA itself passes.
          await expect(page.locator("[data-visual='light-chat-sidebar']:visible")
            .getByAltText("TokenDance")).toHaveCSS("filter", "none");
          await page.locator("[data-visual='light-chat-sidebar']:visible")
            .getByRole("button", { name: /账户菜单|Account menu/ }).click();
          await page.getByRole("menuitem", { name: /登录|Log in|Sign in/ }).click();

          const dialog = page.getByRole("dialog");
          const link = dialog.getByRole("link", {
            name: language === "zh-CN"
              ? "使用 TokenDance Account 继续"
              : "Continue with TokenDance Account",
          });
          await expect(link).toBeVisible();
          await expect(link).toHaveAttribute("href", "/api/oidc/login");
          const mark = link.locator("[data-brand='tokendance-account']");
          await expect(mark).toBeVisible();
          await expect(mark).toHaveAttribute("aria-hidden", "true");
          await expect(mark).toHaveCSS("width", "18px");
          await expect(mark).toHaveCSS("height", "18px");
          await expect(mark).toHaveCSS("mask-image", /\/brand\/tokendance-mark-white-transparent\.svg/);
          await expect(link).toHaveAccessibleName(language === "zh-CN"
            ? "使用 TokenDance Account 继续"
            : "Continue with TokenDance Account");
          // Decode the exact CSS resource: a non-empty mask URL alone also
          // passes when the file is missing and the brand is invisible.
          expect(await mark.evaluate(async (element) => {
            const source = getComputedStyle(element).maskImage.match(/url\(["']?(.*?)["']?\)/)?.[1];
            if (!source) return false;
            const image = new Image();
            image.src = source;
            await image.decode();
            return image.naturalWidth > 0 && image.naturalHeight > 0;
          })).toBe(true);
          await expect(link).toHaveCSS("font-size", "13px");
          await expect.poll(() => contrastRatio(link)).toBeGreaterThanOrEqual(4.5);
          const box = await link.boundingBox();
          expect(box!.height).toBeGreaterThanOrEqual(44);
          expect(box!.width).toBeGreaterThanOrEqual(44);
          expect(box!.x).toBeGreaterThanOrEqual(0);
          expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
          expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
          expect(await page.evaluate(() => document.documentElement.scrollWidth))
            .toBeLessThanOrEqual(page.viewportSize()!.width);
          await expect(dialog.getByRole("tab")).toHaveCount(0);
          await page.screenshot({
            path: test.info().outputPath("account-login.png"),
            animations: "disabled",
          });
          await link.focus();
          await expect(link).toBeFocused();
          await expect.poll(() => contrastRatio(link)).toBeGreaterThanOrEqual(4.5);
          await link.hover();
          await expect.poll(() => contrastRatio(link)).toBeGreaterThanOrEqual(4.5);
        });
      }
    }
  }
});
