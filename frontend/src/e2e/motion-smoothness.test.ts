import { expect, test, type Page, type WebSocketRoute } from "@playwright/test";

test.use({ serviceWorkers: "block" });

type WireEvent = Record<string, unknown> & { type?: string };

async function setupChat(page: Page, history: WireEvent[] = []) {
  const sent: WireEvent[] = [];
  let socket: WebSocketRoute | null = null;

  await page.addInitScript(() => {
    localStorage.setItem("tokendance:lang", "en");
    localStorage.setItem("tdchat-theme", "light");
    localStorage.setItem("tokendance:show-reasoning", "1");
  });
  await page.route("**/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    return route.fulfill({
      json: path === "/api/session"
        ? { authenticated: true, username: "alice", kind: "oidc", csrf_token: "motion-test" }
        : path === "/api/config"
          ? {
              bot_name: "TokenBot",
              model: "motion-test-model",
              llm_enabled: true,
              oidc_enabled: true,
              local_auth_enabled: false,
              capabilities: { run_control: true },
            }
          : { messages: [], users: [] },
    });
  });
  await page.routeWebSocket("**/ws", (ws) => {
    socket = ws;
    ws.onMessage((raw) => {
      const event = JSON.parse(String(raw)) as WireEvent;
      sent.push(event);
      if (event.type === "join") {
        ws.send(JSON.stringify({ type: "history", messages: history }));
      }
    });
  });

  await page.goto("/");
  const input = page.locator("[data-visual='composer-textarea']");
  await expect(input).toBeEnabled();
  return {
    input,
    sent,
    send: (event: WireEvent) => {
      if (!socket) throw new Error("WebSocket route is not connected");
      socket.send(JSON.stringify(event));
    },
  };
}

test("refresh renders recent history without replaying message entrances", async ({ page }) => {
  const text = `history landing ${Date.now()}`;
  await page.addInitScript(() => {
    (window as typeof window & { __historyAnimationStarts?: number })
      .__historyAnimationStarts = 0;
    document.addEventListener("animationstart", (event) => {
      const row = (event.target as Element | null)?.closest?.('[data-visual="message-bubble"]');
      if (row) {
        const target = window as typeof window & { __historyAnimationStarts?: number };
        target.__historyAnimationStarts = (target.__historyAnimationStarts ?? 0) + 1;
      }
    }, true);
  });
  await setupChat(page, [{
    id: "history-motion-message",
    username: "bob",
    content: text,
    timestamp: Date.now() - 1_000,
  }]);
  await expect(page.getByText(text, { exact: true })).toBeVisible();
  await page.waitForTimeout(250);
  expect(await page.evaluate(() =>
    (window as typeof window & { __historyAnimationStarts?: number }).__historyAnimationStarts,
  )).toBe(0);
});

test("direct transcript rendering preserves bottom-follow and scroll-to-bottom", async ({ page }) => {
  const history = Array.from({ length: 40 }, (_, index) => ({
    id: `history-scroll-${index}`,
    username: "bob",
    content: `history row ${index} with enough text to make the transcript scroll`,
    timestamp: Date.now() - (40 - index) * 1_000,
  }));
  const chat = await setupChat(page, history);
  const viewport = page.locator('[role="log"]');
  await expect(page.getByText(history.at(-1)!.content, { exact: true })).toBeVisible();
  await expect.poll(() => viewport.evaluate((element) =>
    element.scrollHeight - element.clientHeight - element.scrollTop,
  )).toBeLessThanOrEqual(2);

  await viewport.evaluate((element) => {
    element.scrollTop = 0;
    element.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
  const scrollToBottom = page.getByRole("button", { name: /Scroll to bottom/i });
  await expect(scrollToBottom).toBeEnabled();
  chat.send({
    type: "message",
    id: "new-scroll-message",
    username: "bob",
    content: "new message while the reader is away from the bottom",
    timestamp: Date.now(),
  });
  await expect(page.getByText("new message while the reader is away from the bottom", { exact: true }))
    .toBeVisible();
  await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBeLessThanOrEqual(2);

  await scrollToBottom.click();
  await expect.poll(() => viewport.evaluate((element) =>
    element.scrollHeight - element.clientHeight - element.scrollTop,
  )).toBeLessThanOrEqual(2);
});

test("own-message echo keeps one DOM row and does not replay its entrance", async ({ page }) => {
  const chat = await setupChat(page);
  const text = `single landing ${Date.now()}`;

  await chat.input.fill(text);
  await chat.input.press("Enter");
  const row = page.locator(
    `[data-visual="message-bubble"][data-message-own="true"]:has-text("${text}")`,
  );
  await expect(row).toHaveCount(1);

  await row.evaluate((element) => {
    const probe = {
      row: element,
      animationStarts: 0,
      opacity: [] as number[],
    };
    (window as typeof window & { __landingProbe?: typeof probe }).__landingProbe = probe;
    document.addEventListener(
      "animationstart",
      (event) => {
        if (probe.row.contains(event.target as Node)) probe.animationStarts += 1;
      },
      true,
    );
    const startedAt = performance.now();
    const sample = (timestamp: number) => {
      probe.opacity.push(Number.parseFloat(getComputedStyle(probe.row).opacity));
      if (timestamp - startedAt < 450) requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });

  await expect.poll(() => chat.sent.find((event) => event.type === "message")?.client_message_id)
    .toBeTruthy();
  const clientMessageId = chat.sent.find((event) => event.type === "message")!
    .client_message_id as string;
  chat.send({
    type: "message",
    id: "persisted-motion-message",
    client_message_id: clientMessageId,
    username: "alice",
    content: text,
    timestamp: Date.now(),
  });

  await expect(page.getByText(text, { exact: true })).toHaveCount(1);
  await page.waitForTimeout(500);
  const result = await page.evaluate(() => {
    const probe = (window as typeof window & {
      __landingProbe?: {
        row: Element;
        animationStarts: number;
        opacity: number[];
      };
    }).__landingProbe!;
    return {
      stillConnected: probe.row.isConnected,
      animationStarts: probe.animationStarts,
      opacity: probe.opacity,
      duplicateRows: [...document.querySelectorAll('[data-visual="message-bubble"]')]
        .filter((element) => element.textContent?.includes(probe.row.textContent || "\u0000"))
        .length,
    };
  });

  expect(result.stillConnected).toBe(true);
  expect(result.duplicateRows).toBe(1);
  expect(result.animationStarts).toBe(0);
  const settledSamples = result.opacity.filter(Number.isFinite);
  expect(settledSamples.some((opacity, index) =>
    index > 0 && opacity + 0.02 < settledSamples[index - 1],
  )).toBe(false);
  // Once the 180ms entrance has settled, acknowledgement must not dim the row.
  const settledOpacity = settledSamples.slice(Math.ceil(settledSamples.length / 3));
  expect(Math.min(...settledOpacity)).toBeGreaterThan(0.95);
});

test("high-frequency tokens reveal in bounded frames without stream remounts", async ({ page }) => {
  const chat = await setupChat(page);
  const token = `stream-${Date.now()}`;

  await page.evaluate(({ token }) => {
    const probe = {
      token,
      updates: [] as Array<{ at: number; length: number }>,
      remounts: 0,
      row: null as Element | null,
      longTasks: [] as Array<{ duration: number; startTime: number }>,
      startedAt: performance.now(),
      scrollJumps: 0,
      lastScrollTop: 0,
    };
    (window as typeof window & { __streamProbe?: typeof probe }).__streamProbe = probe;

    const read = () => {
      const row = [...document.querySelectorAll('[data-visual="message-bubble"]')]
        .find((element) => element.textContent?.includes(token));
      if (!row) return null;
      if (probe.row && probe.row !== row) probe.remounts += 1;
      probe.row = row;
      return row;
    };
    const previousLength = () => probe.updates.at(-1)?.length ?? 0;
    const observer = new MutationObserver(() => {
      const row = read();
      const length = row?.textContent?.length ?? 0;
      if (length > previousLength()) {
        probe.updates.push({ at: performance.now(), length });
        const viewport = document.querySelector('[role="log"]');
        const scrollTop = viewport?.scrollTop ?? 0;
        if (scrollTop + 2 < probe.lastScrollTop) probe.scrollJumps += 1;
        probe.lastScrollTop = scrollTop;
      }
    });
    observer.observe(document.body, {
      childList: true,
      characterData: true,
      subtree: true,
    });
    try {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          if (entry.startTime >= probe.startedAt) {
            probe.longTasks.push({ duration: entry.duration, startTime: entry.startTime });
          }
        }
      }).observe({ type: "longtask", buffered: true });
    } catch {
      // Long Task Timing is Chromium-only; the cadence assertion still applies.
    }
  }, { token });

  chat.send({
    type: "bot_run",
    run_id: "motion-stream",
    message_id: "motion-stream-message",
    stream_id: "motion-stream",
    trigger_message_id: "missing-trigger",
    requested_by: "alice",
    room_id: "public-room",
    status: "running",
    timestamp: Date.now(),
  });
  const typing = page.locator('[data-visual="assistant-typing-bubble"]');
  await expect(typing).toBeVisible();
  const typingSurface = await typing.evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      background: style.backgroundColor,
      borderRadius: style.borderRadius,
    };
  });
  expect(typingSurface.background).toBe("rgba(0, 0, 0, 0)");
  expect(typingSurface.borderRadius).toBe("0px");
  const dotStyles = await typing.locator("[data-active]").evaluateAll((elements) =>
    elements.map((element) => {
      const style = getComputedStyle(element);
      return {
        animationName: style.animationName,
        delay: style.animationDelay,
        color: style.backgroundColor,
      };
    }),
  );
  expect(dotStyles.map((style) => style.animationName)).toEqual(["bounce", "bounce", "bounce"]);
  expect(dotStyles.map((style) => style.delay)).toEqual(["0s", "0.15s", "0.3s"]);
  const firstDotColor = dotStyles[0].color;
  await page.waitForTimeout(420);
  expect(await typing.locator("[data-active]").first().evaluate((element) =>
    getComputedStyle(element).backgroundColor,
  )).not.toBe(firstDotColor);

  const words = Array.from({ length: 120 }, (_, index) => {
    if (index === 0) return `${token}\n\n`;
    if (index % 24 === 0) return `\n## Section ${index / 24}\n`;
    if (index % 17 === 0) return `- item ${index} with \`inline code\`\n`;
    return `word${index} `;
  });
  for (const word of words) {
    chat.send({
      type: "stream",
      run_id: "motion-stream",
      message_id: "motion-stream-message",
      stream_id: "motion-stream",
      username: "TokenBot",
      kind: "text",
      content: word,
    });
  }

  await expect.poll(async () => page.evaluate(({ token }) => {
    const probe = (window as typeof window & {
      __streamProbe?: { row: Element | null };
    }).__streamProbe;
    return Boolean(probe?.row?.textContent?.includes(token));
  }, { token })).toBe(true);
  await expect(typing).toHaveCount(0);
  await page.waitForTimeout(1_200);

  const result = await page.evaluate(() => {
    const probe = (window as typeof window & {
      __streamProbe?: {
        updates: Array<{ at: number; length: number }>;
        remounts: number;
        row: Element | null;
        longTasks: Array<{ duration: number; startTime: number }>;
      scrollJumps: number;
      };
    }).__streamProbe!;
    return {
      updates: probe.updates,
      remounts: probe.remounts,
      connected: probe.row?.isConnected ?? false,
      longTasks: probe.longTasks,
      scrollJumps: probe.scrollJumps,
    };
  });

  expect(result.connected).toBe(true);
  expect(result.remounts).toBe(0);
  expect(result.scrollJumps).toBe(0);
  // 120 synchronous deltas must not become 120 Markdown parses/commits.
  expect(result.updates.length).toBeLessThanOrEqual(45);
  expect(result.longTasks.filter(({ duration }) => duration > 80)).toHaveLength(0);
  const intervals = result.updates.slice(1).map((item, index) =>
    item.at - result.updates[index].at,
  );
  // The render cadence is frame-bounded rather than driven by packet arrival.
  expect(intervals.filter((interval) => interval < 16).length).toBeLessThanOrEqual(2);
});
