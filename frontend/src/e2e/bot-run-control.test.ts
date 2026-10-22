import { expect, test, type WebSocketRoute } from "@playwright/test";

test.use({ serviceWorkers: "block", contextOptions: { reducedMotion: "reduce" } });

for (const width of [1440, 390]) for (const theme of ["light", "dark"]) for (const language of ["zh-CN", "en-US"]) {
  test(`run control ${width} ${theme} ${language}`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
    await page.setViewportSize({ width, height: width < 768 ? 844 : 900 });
    await page.addInitScript(({ theme, language }) => {
      localStorage.setItem("tokendance:lang", language);
      localStorage.setItem("tdchat-theme", theme);
    }, { theme, language });
    const base = { type: "bot_run", requested_by: "alice", room_id: "public-room", trigger_message_id: "trigger", timestamp: Date.now() };
    const own = { ...base, run_id: "own", message_id: "own-message", stream_id: "own", status: "running" };
    const peer = { ...base, run_id: "peer", message_id: "peer-message", stream_id: "peer", status: "queued", requested_by: "bob", trigger_message_id: "other-trigger" };
    const privateRun = { ...base, run_id: "private", message_id: "private-message", stream_id: "private", status: "queued", private: true, to: "alice" };
    const history = [
      { id: "trigger", username: "alice", content: "@TokenBot Explain the release checklist", timestamp: Date.now() - 10000 },
      { id: "other-trigger", username: "bob", content: "@TokenBot Review the next change", timestamp: Date.now() - 5000 },
    ];
    await page.route("**/api/**", (route) => {
      const path = new URL(route.request().url()).pathname;
      return route.fulfill({ json: path === "/api/session"
        ? { authenticated: true, username: "alice", kind: "oidc", csrf_token: "mock-local-only" }
        : path === "/api/config"
          ? { bot_name: "TokenBot", model: "test-model", llm_enabled: true, oidc_enabled: true, local_auth_enabled: false, capabilities: { run_control: true } }
          : { messages: [], users: [] } });
    });
    let socket: WebSocketRoute;
    let connections = 0;
    let snapshot: Record<string, unknown>[] = [own, peer, privateRun];
    const sent: Record<string, unknown>[] = [];
    await page.routeWebSocket("**/ws", (ws) => {
      socket = ws;
      ws.onMessage((raw) => {
        const event = JSON.parse(String(raw));
        sent.push(event);
        if (event.type === "join") {
          connections++;
          ws.send(JSON.stringify({ type: "history", messages: history }));
          for (const run of snapshot) ws.send(JSON.stringify(run));
        }
      });
    });
    const send = (event: Record<string, unknown>) => socket!.send(JSON.stringify(event));
    const strip = page.locator('[data-visual="bot-run-strip"]');
    const row = (id: string) => strip.locator(`[data-run-id="${id}"]`);
    const stop = (id: string) => row(id).getByRole("button");
    const selectSpace = async (privateChat: boolean) => {
      if (width < 768) await page.getByLabel(/打开侧边栏|Open sidebar/).click();
      const sidebar = page.locator('[data-visual="light-chat-sidebar"]:visible');
      await sidebar.getByRole("button", { name: privateChat ? /TokenBot/ : /公共聊天|Public Chat/i }).click();
    };

    await page.goto("/");
    await expect(page.locator('[data-visual="composer-textarea"]')).toBeEnabled();
    await expect(row("own")).toBeVisible();
    await expect(row("peer")).toBeVisible();
    await expect(row("private")).toHaveCount(0);
    await expect(row("peer").getByRole("button")).toHaveCount(0);
    send({ type: "stream", username: "TokenBot", run_id: "own", message_id: "own-message", stream_id: "own", kind: "text", content: "A partial reply" });
    send({ type: "stream", username: "TokenBot", run_id: "own", message_id: "own-message", stream_id: "own", kind: "reasoning", content: "PRIVATE REASONING MUST NOT APPEAR" });
    await expect(page.getByText("A partial reply", { exact: true })).toBeVisible();
    await expect(page.getByText("PRIVATE REASONING MUST NOT APPEAR")).toHaveCount(0);
    await page.screenshot({ path: test.info().outputPath("public-runs.png"), fullPage: true, animations: "disabled" });
    await stop("own").focus();
    await page.keyboard.press("Enter");
    await expect(stop("own")).toBeDisabled();
    expect(sent.filter((event) => event.type === "bot_cancel")).toEqual([{ type: "bot_cancel", run_id: "own" }]);
    send({ ...own, status: "cancelled" });
    for (let i = 0; i < 50; i++) send({ type: "stream", username: "TokenBot", run_id: "own", message_id: "own-message", stream_id: "own", kind: "text", content: "late cancelled output" });
    send({ type: "message", id: "own-message", run_id: "own", message_id: "own-message", stream_id: "own", username: "TokenBot", content: "late cancelled final", timestamp: Date.now() });
    send(own);
    await expect(row("own")).toHaveCount(0);
    await expect(page.getByText(/late cancelled/)).toHaveCount(0);
    await expect(page.getByText("A partial reply", { exact: true })).toBeVisible();
    await expect(page.locator('[data-visual="message-stopped"]')).toHaveCount(1);
    await expect(row("peer")).toBeVisible();

    await selectSpace(true);
    await expect(row("private")).toBeVisible();
    await expect(row("peer")).toHaveCount(0);
    await page.screenshot({ path: test.info().outputPath("private-queued.png"), fullPage: true, animations: "disabled" });
    send({ ...privateRun, status: "running" });
    // The 1:1 composer owns the stop affordance; the status strip is text only.
    await expect(row("private").getByRole("button")).toHaveCount(0);
    const composerStop = page.locator("[data-visual='composer-stop']");
    await expect(composerStop).toBeVisible();
    await composerStop.click();
    expect(sent.filter((event) => event.type === "bot_cancel").at(-1)).toEqual({ type: "bot_cancel", run_id: "private" });
    send({ type: "stream", username: "TokenBot", run_id: "private", message_id: "private-message", stream_id: "private", private: true, to: "alice", content: "Private draft" });
    await expect(page.getByText("Private draft", { exact: true })).toBeVisible();
    send({ ...privateRun, status: "completed" });
    const privateFinal = { type: "message", id: "private-message", run_id: "private", message_id: "private-message", stream_id: "private", private: true, to: "alice", username: "TokenBot", content: "Private final", timestamp: Date.now() };
    send(privateFinal); send(privateFinal);
    await expect(page.getByText("Private final", { exact: true })).toHaveCount(1);
    await expect(page.getByText("Private draft", { exact: true })).toHaveCount(0);
    await expect(row("private")).toHaveCount(0);
    await selectSpace(false);
    await expect(page.getByText("Private final", { exact: true })).toHaveCount(0);

    // A reconnect clears active UI; authoritative snapshots restore only visible runs.
    snapshot = [{ ...own, status: "cancelled" }, { ...peer, status: "running" }];
    await socket!.close({ code: 1012, reason: "local test reconnect" });
    await expect(strip).toHaveCount(0);
    await expect.poll(() => connections).toBe(2);
    await expect(row("peer")).toBeVisible();
    await expect(row("own")).toHaveCount(0);
    send({ ...peer, status: "completed" });
    const final = { type: "message", id: "peer-message", run_id: "peer", message_id: "peer-message", stream_id: "peer", username: "TokenBot", content: "One completed public reply", timestamp: Date.now() };
    send(final); send(final);
    await expect(page.getByText("One completed public reply", { exact: true })).toHaveCount(1);
    await expect(strip).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    expect(errors).toEqual([]);
  });
}
