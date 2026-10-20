import { expect, test } from "@playwright/test";

test.describe("lightweight chat surface", () => {
  for (const { mobile, language, theme } of [
    { mobile: false, language: "en-US", theme: "light" },
    { mobile: true, language: "zh-CN", theme: "light" },
    { mobile: false, language: "zh-CN", theme: "dark" },
    { mobile: true, language: "en-US", theme: "dark" },
  ]) {
    test(`public timeline stays docked and AI onboarding stays private on ${mobile ? "mobile" : "desktop"} ${language} ${theme}`, async ({ page }) => {
      if (mobile) await page.setViewportSize({ width: 390, height: 844 });
      await page.addInitScript(({ language, theme }) => {
        localStorage.setItem("tokendance:lang", language);
        localStorage.setItem("tdchat-theme", theme);
      }, { language, theme });
      await page.route("**/api/messages*", (route) => route.fulfill({ json: [] }));
      await page.routeWebSocket("**/ws", (ws) => {
        const server = ws.connectToServer();
        server.onMessage((raw) => {
          const message = JSON.parse(String(raw));
          ws.send(JSON.stringify(message.type === "history" ? { ...message, messages: [] } : message));
        });
      });
      await page.goto("/");
      const timeline = page.locator("[data-visual='room-timeline']");
      await expect(timeline).toBeVisible();
      await expect(page.getByRole("textbox")).toBeEnabled();
      await expect(timeline.getByRole("heading")).toHaveCount(0);
      await expect(timeline.getByRole("button", { name: /What can you do|你能做什么/ })).toHaveCount(0);
      const dock = page.locator("[data-visual='room-composer-dock']");
      const bounds = await dock.boundingBox();
      expect(bounds).not.toBeNull();
      expect(Math.abs(page.viewportSize()!.height - bounds!.y - bounds!.height)).toBeLessThan(25);
      await page.screenshot({ path: test.info().outputPath("public-timeline-empty.png"), fullPage: true, animations: "disabled" });
      const text = `timeline message ${Date.now()}`;
      await page.getByRole("textbox").fill(text);
      await page.locator("[data-visual='composer-send']").click();
      await expect(timeline.getByText(text)).toBeVisible();
      if (mobile) await page.getByLabel(/Open sidebar|打开侧边栏/).click();
      await page.locator("[data-visual='light-chat-sidebar']:visible").getByRole("button", { name: /TokenBot/ }).click();
      await expect(page.locator("[data-visual='assistant-thread']").getByRole("heading")).toBeVisible();
      await expect(page.getByRole("button", { name: /What can you do|你能做什么/ })).toBeVisible();
      await page.screenshot({ path: test.info().outputPath("private-assistant-empty.png"), fullPage: true, animations: "disabled" });
      if (mobile) await page.getByLabel(/Open sidebar|打开侧边栏/).click();
      await page.locator("[data-visual='light-chat-sidebar']:visible").getByRole("button", { name: /Public Chat|公共聊天/ }).click();
      await expect(timeline.getByText(text)).toBeVisible();
    });
  }

  for (const mobile of [false, true]) {
    test(`source and license link is accessible on ${mobile ? "mobile" : "desktop"}`, async ({ page }) => {
      if (mobile) await page.setViewportSize({ width: 390, height: 844 });
      await page.goto("/");
      if (mobile) await page.getByLabel(/Open sidebar|打开侧边栏/).click();
      const sidebar = page.locator("[data-visual='light-chat-sidebar']:visible");
      await sidebar.getByRole("button", { name: /Account menu|账户菜单/ }).click();
      const source = page.getByRole("menuitem", { name: /Source and license|源码与许可证/ });
      await expect(source).toBeVisible();
      await expect(source).toHaveAttribute("href", process.env.E2E_SOURCE_URL || "https://github.com/TokenDanceLab/tokendance-chat-source");
      await expect(source).toHaveAttribute("rel", "noopener noreferrer");
      await page.screenshot({ path: test.info().outputPath("source-menu.png"), fullPage: true, animations: "disabled" });
    });
  }

  test("desktop keeps only public room and AI workspaces", async ({ page }) => {
    await page.goto("/");

    await expect(page.getByText("TokenDance", { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: /Public Chat|公共聊天/ }).first()).toBeVisible();
    await expect(page.getByText("TokenBot").first()).toBeVisible();

    const text = `codex guest send ${Date.now()}`;
    const composer = page.getByRole("textbox");
    await expect(composer).toBeEnabled({ timeout: 15000 });
    await composer.fill(text);
    await page.locator("[data-visual='composer-send']").click();
    await expect(page.getByText(text)).toBeVisible({ timeout: 15000 });

    const aliasText = `codex legacy alias ${Date.now()} asks @webuibot for TokenBot`;
    await composer.fill(aliasText);
    await page.locator("[data-visual='composer-send']").click();
    await expect(page.getByText(/@TokenBot/).last()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText(/WebUIChat|WebUIBot|webuichat|webuibot/)).toHaveCount(0);

    await page.reload();
    await expect(page.getByText(text)).toBeVisible({ timeout: 15000 });

    await expect(page.getByText(/好友|Friends/)).toHaveCount(0);
    await expect(page.getByText(/群组|Groups/)).toHaveCount(0);
    await expect(page.getByText(/私信|Direct Message|DM/)).toHaveCount(0);
    await expect(page.getByRole("button", { name: /语音通话|Voice Call/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /视频通话|Video Call/ })).toHaveCount(0);

    const sidebar = page.locator("[data-visual='light-chat-sidebar']");

    await sidebar.getByRole("button", { name: /TokenBot/ }).click();
    await expect(page.locator("[data-visual='ai-chat-workbench']")).toHaveCount(0);
    await expect(page.locator("[data-visual='assistant-switch']")).toHaveCount(0);
    const composerContext = page.locator("[data-visual='composer-ai-context']");
    await expect(composerContext).toBeVisible();
    await expect(composerContext.getByText("TokenBot")).toBeVisible();
    await expect(page.getByText(/Knowledge|Tools|Prompts/)).toHaveCount(0);
    const tokenBotText = `tokenbot route ${Date.now()}`;
    await composer.fill(tokenBotText);
    await page.locator("[data-visual='composer-send']").click();
    await expect(page.getByText(tokenBotText).first()).toBeVisible({ timeout: 15000 });
    // Private 1:1: the message is sent as a direct message (to: TokenBot) and is
    // NOT prefixed with an @mention.
    await expect(page.getByText(/@TokenBot/)).toHaveCount(0);
  });

  test("mobile sidebar and composer stay compact", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/");

    await page.getByLabel(/Open sidebar|打开侧边栏/).click();
    const sidebar = page.locator("[data-visual='light-chat-sidebar']:visible");
    await expect(sidebar).toBeVisible();
    await expect(sidebar.getByText("TokenBot").first()).toBeVisible();

    await sidebar.getByRole("button", { name: /TokenBot/ }).click();
    await expect(page.locator("[data-visual='ai-chat-workbench']")).toHaveCount(0);
    await expect(page.locator("[data-visual='composer-ai-context']")).toContainText("TokenBot");
    await expect(page.locator("[data-visual='composer-card']")).toBeVisible();
    await expect(page.getByRole("button", { name: /录制语音|Record voice/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /定时发送消息|Schedule Message/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "GIF" })).toHaveCount(0);
  });
});
