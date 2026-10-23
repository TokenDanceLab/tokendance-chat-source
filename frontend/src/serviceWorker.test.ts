import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("service worker cache contract", () => {
  it("invalidates the pre-send-motion shell cached by users", () => {
    const source = readFileSync(resolve(process.cwd(), "public/sw.js"), "utf8");
    const version = source.match(/const CACHE_NAME = "tdchat-v(\d+)";/)?.[1];

    expect(version).toBeDefined();
    expect(Number(version)).toBeGreaterThanOrEqual(19);
  });
});
