import { expect, test } from "@playwright/test";

test.use({ serviceWorkers: "block" });

for (const width of [1440, 390]) for (const theme of ["light", "dark"]) {
  test(`mention avatars and keyboard insertion ${width} ${theme}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width < 768 ? 844 : 900 });
    await page.addInitScript((theme) => {
      localStorage.setItem("tokendance:lang", "en-US");
      localStorage.setItem("tdchat-theme", theme);
    }, theme);
    const avatar = "/test-avatar.svg";
    await page.route("**/test-avatar.svg", (route) => route.fulfill({ contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24"><rect width="24" height="24" fill="#0074e8"/></svg>' }));
    await page.route("**/api/**", (route) => {
      const path = new URL(route.request().url()).pathname;
      return route.fulfill({ json: path === "/api/session"
        ? { authenticated: true, username: "viewer", kind: "oidc", csrf_token: "mock-local-only" }
        : path === "/api/config"
          ? { bot_name: "TokenBot", model: "test-model", llm_enabled: true, capabilities: { run_control: true } }
          : { messages: [], users: [] } });
    });
    const sent: Record<string, unknown>[] = [];
    await page.routeWebSocket("**/ws", (ws) => ws.onMessage((raw) => {
      const event = JSON.parse(String(raw));
      sent.push(event);
      if (event.type !== "join") return;
      ws.send(JSON.stringify({ type: "history", messages: [] }));
      ws.send(JSON.stringify({ type: "online_users", online: ["viewer", "VIEWER", "alice", "guest_visual"] }));
      ws.send(JSON.stringify({ type: "profile_updated", username: "alice", display_name: "Alice Example", avatar_url: avatar }));
    }));
    await page.goto("/");
    const input = page.locator('[data-visual="composer-textarea"]');
    await expect(input).toBeEnabled();
    await input.pressSequentially("@");
    const human = page.getByRole("option", { name: "Alice Example" });
    const assistant = page.getByRole("option", { name: "TokenBot" });
    await expect(page.getByRole("option", { name: "viewer" })).toHaveCount(0);
    await expect(page.getByRole("option", { name: "VIEWER" })).toHaveCount(0);
    await expect(human.locator("img")).toHaveAttribute("src", avatar);
    expect(await human.locator("img").evaluate((image) => (image as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    await expect(human.locator('[data-slot="avatar-fallback"]')).toHaveCount(0);
    await expect(assistant.locator("svg")).toBeVisible();
    await expect(human).toHaveAttribute("aria-selected", "true");
    await input.press("ArrowDown");
    await input.press("ArrowDown");
    await expect(assistant).toHaveAttribute("aria-selected", "true");
    await input.press("Enter");
    await expect(input).toHaveValue("@TokenBot ");
    expect(sent.filter((event) => event.type === "message")).toHaveLength(0);
    await input.fill("@a");
    await human.click();
    await expect(input).toHaveValue("@alice ");
    await expect(page.getByRole("listbox")).toHaveCount(0);
    await input.fill("@");
    const guest = page.getByRole("option", { name: "guest_visual" });
    await expect(guest.locator('[data-visual="guest-avatar"]')).toBeVisible();
    // Avatar frames stay concentric squares; a 1px offset or non-square
    // content box makes the ring cover the graphic (regression: 2026-10-23).
    const geometry = await page.evaluate(() => [...document.querySelectorAll('[data-slot="avatar"]')].map((el) => {
      const r = el.getBoundingClientRect();
      const inner = el.querySelector('[data-slot="avatar-fallback"], img');
      const ir = inner?.getBoundingClientRect();
      return { w: r.width, h: r.height, innerW: ir?.width ?? 0, innerH: ir?.height ?? 0,
        dx: ir ? Math.abs(ir.x + ir.width / 2 - (r.x + r.width / 2)) : 0,
        dy: ir ? Math.abs(ir.y + ir.height / 2 - (r.y + r.height / 2)) : 0 };
    }));
    expect(geometry.length).toBeGreaterThan(0);
    for (const g of geometry) {
      expect(Math.abs(g.w - g.h)).toBeLessThan(0.6);
      expect(Math.abs(g.innerW - g.innerH)).toBeLessThan(0.6);
      expect(g.dx).toBeLessThan(0.6);
      expect(g.dy).toBeLessThan(0.6);
    }
    const graphic = await guest.locator('[data-visual="guest-avatar"]').evaluate((svg) => svg.outerHTML);
    await page.reload();
    await expect(input).toBeEnabled();
    await input.fill("@");
    await expect(guest.locator('[data-visual="guest-avatar"]')).toBeVisible();
    expect(await guest.locator('[data-visual="guest-avatar"]').evaluate((svg) => svg.outerHTML)).toBe(graphic);
    await input.fill("@");
    await input.press("Escape");
    await expect(page.getByRole("listbox")).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: test.info().outputPath("mention-canonical-insertion.png"), fullPage: true });
  });
}
