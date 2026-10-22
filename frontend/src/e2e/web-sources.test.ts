import { expect, test, type WebSocketRoute } from "@playwright/test";

test.use({ serviceWorkers: "block", contextOptions: { reducedMotion: "reduce" } });

for (const width of [1440, 390, 320]) for (const theme of ["light", "dark"]) for (const language of ["zh-CN", "en-US"]) {
  test(`web sources ${width} ${theme} ${language}`, async ({ page, context }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
    await page.setViewportSize({ width, height: width < 768 ? 844 : 900 });
    await page.addInitScript(({ theme, language }) => {
      localStorage.setItem("tokendance:lang", language);
      localStorage.setItem("tdchat-theme", theme);
    }, { theme, language });
    const source = { type: "source", source_id: "read-uuid", title: "OpenAI documentation — response streaming and API updates", url: "https://example.com/docs", status: "fetched", cited: true, query: "RAW QUERY", result: "RAW PAGE" };
    const sources = [
      source, source,
      { ...source, source_id: "found-uuid", title: "Candidate page with a long title that must wrap at narrow widths", url: "https://example.org/candidate", status: "discovered", cited: false },
      { ...source, source_id: "failed-uuid", title: "Page could not be retrieved", url: "https://example.net/failed", status: "failed", cited: false },
      { ...source, source_id: "bad-uuid", title: "UNSAFE SOURCE", url: "javascript:alert(1)" },
    ];
    const answer = "The documentation describes the streaming API. [1](https://example.com/docs)";
    const final = {
      type: "message", id: "answer", run_id: "answered", message_id: "answer", stream_id: "answered", username: "TokenBot", content: answer, timestamp: Date.now(),
      parts: [{ type: "text", text: answer }, { type: "reasoning", text: "RAW REASONING" }, { type: "tool-call", tool_name: "web_search", arguments: "RAW TOOL", status: "running" }, ...sources],
    };
    const privateFinal = { ...final, id: "private-answer", run_id: "private-run", message_id: "private-answer", stream_id: "private-run", private: true, to: "alice", content: "Private answer", parts: [{ type: "text", text: "Private answer" }, { ...source, title: "Private source", source_id: "private-source" }] };
    const history = [
      { id: "trigger", username: "alice", content: "@TokenBot Find the streaming documentation", timestamp: Date.now() - 10000 },
      { id: "plain", username: "TokenBot", content: "[Ordinary model link](https://example.com/ordinary)", timestamp: Date.now() - 360000 },
      final,
    ];
    const run = { type: "bot_run", requested_by: "alice", trigger_message_id: "trigger", run_id: "cancel", message_id: "cancel-answer", stream_id: "cancel", status: "running", timestamp: Date.now() };
    let capability = true;
    await page.route("**/api/**", (route) => {
      const url = new URL(route.request().url());
      return route.fulfill({ json: url.pathname === "/api/session"
        ? { authenticated: true, username: "alice", kind: "oidc", csrf_token: "local-only" }
        : url.pathname === "/api/config"
          ? { bot_name: "TokenBot", model: "deepseek-v4-flash", llm_enabled: true, oidc_enabled: true, local_auth_enabled: false, capabilities: { run_control: true, web_search: capability } }
          : { messages: url.searchParams.has("to") ? [privateFinal] : [], users: [] } });
    });
    await context.route("https://example.com/**", (route) => route.fulfill({ contentType: "text/html", body: "<title>Local source fixture</title>Source page" }));
    let socket: WebSocketRoute;
    await page.routeWebSocket("**/ws", (ws) => {
      socket = ws;
      ws.onMessage((raw) => {
        const event = JSON.parse(String(raw));
        if (event.type === "join") ws.send(JSON.stringify({ type: "history", messages: history }));
        if (event.type === "bot_cancel") ws.send(JSON.stringify({ ...run, status: "cancelled" }));
      });
    });
    const send = (event: Record<string, unknown>) => socket!.send(JSON.stringify(event));
    const lists = page.locator('[data-visual="message-sources"]');
    const selectSpace = async (privateChat: boolean) => {
      if (width < 768) await page.getByLabel(/打开侧边栏|Open sidebar/).click();
      await page.locator('[data-visual="light-chat-sidebar"]:visible').getByRole("button", { name: privateChat ? /TokenBot/ : /公共聊天|Public Chat/i }).click();
    };
    await page.goto("/");
    await expect(page.locator('[data-visual="composer-textarea"]')).toBeEnabled();
    await expect(lists).toHaveCount(1);
    await expect(lists.getByRole("link")).toHaveCount(3);
    await expect(lists.getByText(language === "zh-CN" ? "仅发现 · 未读取" : "Discovered · Not read")).toBeVisible();
    await expect(lists.getByText(language === "zh-CN" ? "读取失败" : "Could not read")).toBeVisible();
    await expect(lists.getByText(language === "zh-CN" ? "已引用" : "Cited", { exact: true })).toHaveCount(1);
    await expect(page.getByText(/RAW QUERY|RAW PAGE|RAW REASONING|RAW TOOL|UNSAFE SOURCE/)).toHaveCount(0);
    const firstLink = lists.getByRole("link").first();
    await firstLink.focus();
    await expect(firstLink).toBeFocused();
    expect((await firstLink.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    const popupPromise = page.waitForEvent("popup");
    await page.keyboard.press("Enter");
    const popup = await popupPromise;
    await expect(popup).toHaveURL("https://example.com/docs");
    await popup.close();
    await page.screenshot({ path: test.info().outputPath("sources-public.png"), fullPage: true, animations: "disabled" });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    // Duplicate final/done/history deliveries preserve one list; cancellation fences late sources.
    send(final); send({ ...final, type: "stream", kind: "done" });
    await expect(lists).toHaveCount(1);
    send(run);
    send({ type: "stream", username: "TokenBot", run_id: "cancel", stream_id: "cancel", message_id: "cancel-answer", content: "Draft to stop", kind: "text" });
    await page.locator('[data-run-id="cancel"]').getByRole("button").click();
    await expect(page.locator('[data-run-id="cancel"]')).toHaveCount(0);
    const cancelled = { ...final, id: "cancel-answer", message_id: "cancel-answer", run_id: "cancel", stream_id: "cancel", content: "CANCELLED ANSWER", parts: [{ ...source, title: "CANCELLED SOURCE" }] };
    send(cancelled); send({ ...cancelled, type: "stream", kind: "done" });
    // Stopping retains the text already delivered, but never late answers or sources.
    await expect(page.getByText("Draft to stop", { exact: true })).toBeVisible();
    await expect(page.locator('[data-visual="message-stopped"]')).toBeVisible();
    await expect(page.getByText(/CANCELLED SOURCE|CANCELLED ANSWER/)).toHaveCount(0);
    send(privateFinal);
    await expect(page.getByText("Private source")).toHaveCount(0);
    await selectSpace(true);
    const searchCapability = page.getByText(/可搜索网页|Web search available/).first();
    if (width >= 1368) {
      // Wide layouts intentionally hide header metadata in favor of the rail.
      await expect(searchCapability).toBeHidden();
      await expect(searchCapability).toHaveText(/可搜索网页|Web search available/);
    } else {
      await expect(searchCapability).toBeVisible();
    }
    await expect(lists).toHaveCount(1);
    await expect(lists.getByText("Private source")).toBeVisible();
    await expect(lists.getByText(source.title)).toHaveCount(0);
    await page.screenshot({ path: test.info().outputPath("sources-private.png"), fullPage: true, animations: "disabled" });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await selectSpace(false);
    capability = false;
    await page.reload();
    await expect(lists).toHaveCount(1);
    await expect(lists.getByRole("link")).toHaveCount(3);
    await selectSpace(true);
    await expect(lists.getByText("Private source")).toBeVisible();
    await expect(page.getByText(/可搜索网页|Web search available/)).toHaveCount(0);
    expect(errors).toEqual([]);
  });
}
