import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  avatarIdentityStyle,
  cn,
  formatDate,
  formatFullTime,
  formatTime,
  hashString,
  plainTextPreview,
  type TFunction,
} from "@/lib/utils";

// --- Mock t functions that mirror the profile.* keys in translations.ts ---

const t_zh: TFunction = (key: string, params?: Record<string, string | number>): string => {
  const dict: Record<string, string> = {
    "profile.justNow": "刚刚",
    "profile.minutesAgo": "{{n}}分钟前",
    "profile.today": "今天",
    "profile.yesterday": "昨天",
  };
  let val = dict[key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      val = val.replace(`{{${k}}}`, String(v));
    }
  }
  return val;
};

const t_en: TFunction = (key: string, params?: Record<string, string | number>): string => {
  const dict: Record<string, string> = {
    "profile.justNow": "just now",
    "profile.minutesAgo": "{{n}}m ago",
    "profile.today": "Today",
    "profile.yesterday": "Yesterday",
  };
  let val = dict[key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      val = val.replace(`{{${k}}}`, String(v));
    }
  }
  return val;
};

describe("formatTime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Freeze "now" to 2025-01-15T12:00:00.000Z
    vi.setSystemTime(new Date("2025-01-15T12:00:00.000Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns i18n justNow when less than 60 seconds", () => {
    const ts = Date.now() - 30 * 1000; // 30 seconds ago
    expect(formatTime(ts, t_zh)).toBe("刚刚");
    expect(formatTime(ts, t_en)).toBe("just now");
  });

  it("returns i18n minutesAgo when 1 to 59 minutes", () => {
    const ts = Date.now() - 5 * 60 * 1000; // 5 minutes ago
    expect(formatTime(ts, t_zh)).toBe("5分钟前");
    expect(formatTime(ts, t_en)).toBe("5m ago");
  });

  it("shows HH:mm when within same day and less than 4 hours", () => {
    // 2 hours ago — still within the 4-hour window
    const twoHoursAgo = Date.now() - 2 * 60 * 60 * 1000;
    const expected = new Date(twoHoursAgo);
    const pad = (n: number) => String(n).padStart(2, "0");
    expect(formatTime(twoHoursAgo, t_zh)).toBe(`${pad(expected.getHours())}:${pad(expected.getMinutes())}`);
  });

  it("shows MM-DD when over 4 hours", () => {
    // 5 days ago — well past the 4-hour window
    const fiveDaysAgo = Date.now() - 5 * 24 * 60 * 60 * 1000;
    const d = new Date(fiveDaysAgo);
    const pad = (n: number) => String(n).padStart(2, "0");
    expect(formatTime(fiveDaysAgo, t_zh)).toBe(`${pad(d.getMonth() + 1)}-${pad(d.getDate())}`);
  });

  it("shows YY-MM-DD when different year", () => {
    // Create a timestamp in the previous year
    const now = new Date();
    const lastYear = new Date(now.getFullYear() - 1, 5, 15, 12, 0, 0);
    const pad = (n: number) => String(n).padStart(2, "0");
    const yy = String(lastYear.getFullYear()).slice(-2);
    expect(formatTime(lastYear.getTime(), t_zh)).toBe(
      `${yy}-${pad(lastYear.getMonth() + 1)}-${pad(lastYear.getDate())}`,
    );
  });
});

describe("cn", () => {
  it.each(["micro", "label", "name"])(
    "keeps text-%s font size independent from foreground color",
    (size) => {
      for (const color of ["primary-foreground", "secondary-foreground", "muted-foreground/70"]) {
        expect(cn(`text-${color}`, `text-${size}`)).toBe(`text-${color} text-${size}`);
        expect(cn(`text-${size}`, `text-${color}`)).toBe(`text-${size} text-${color}`);
      }
      expect(cn("text-sm", `text-${size}`)).toBe(`text-${size}`);
      expect(cn(`text-${size}`, "text-lg")).toBe("text-lg");
      expect(cn("hover:text-primary-foreground", `hover:text-${size}`)).toBe(
        `hover:text-primary-foreground hover:text-${size}`,
      );
    },
  );

  it("合并多个类名", () => {
    const result = cn("px-4", "py-2", "bg-red-500");
    expect(result).toContain("px-4");
    expect(result).toContain("py-2");
    expect(result).toContain("bg-red-500");
  });

  it("处理条件类名（falsy值被过滤）", () => {
    // eslint-disable-next-line no-constant-binary-expression
    const result = cn("base", false && "hidden", "visible");
    expect(result).toContain("base");
    expect(result).toContain("visible");
    expect(result).not.toContain("hidden");
  });

  it("tailwind-merge 合并冲突类名", () => {
    // twMerge should resolve conflicts: later classes override earlier ones
    const result = cn("px-4", "px-6");
    expect(result).toContain("px-6");
    expect(result).not.toContain("px-4");
  });

  it("无参数时返回空字符串", () => {
    expect(cn()).toBe("");
  });

  it("过滤 undefined 和 null", () => {
    const result = cn("base", undefined, null);
    expect(result).toBe("base");
  });

  it("展开数组参数", () => {
    const result = cn("base", ["px-4", "py-2"]);
    expect(result).toContain("base");
    expect(result).toContain("px-4");
    expect(result).toContain("py-2");
  });

  it("处理对象形式的条件类名", () => {
    const result = cn("base", { active: true, disabled: false, hidden: false });
    expect(result).toContain("base");
    expect(result).toContain("active");
    expect(result).not.toContain("disabled");
    expect(result).not.toContain("hidden");
  });
});

describe("hashString", () => {
  it("相同输入产生相同哈希", () => {
    expect(hashString("Alice")).toBe(hashString("Alice"));
  });

  it("不同输入产生不同哈希", () => {
    expect(hashString("Alice")).not.toBe(hashString("Bob"));
  });

  it("返回非负整数", () => {
    const h = hashString("test");
    expect(h).toBeGreaterThanOrEqual(0);
    expect(Number.isInteger(h)).toBe(true);
  });

  it("空字符串返回0", () => {
    expect(hashString("")).toBe(0);
  });
});

describe("avatarIdentityStyle", () => {
  it("相同用户名返回相同色相变量", () => {
    expect(avatarIdentityStyle("Alice")).toEqual(avatarIdentityStyle("Alice"));
  });

  it("返回可用于CSS变量的色相", () => {
    const style = avatarIdentityStyle("Alice") as Record<string, string>;
    expect(Number(style["--chat-identity-hue"])).toBeGreaterThanOrEqual(0);
    expect(Number(style["--chat-identity-hue"])).toBeLessThan(360);
  });
});

// ── formatFullTime ──────────────────────────────────────────────────
describe("formatFullTime", () => {
  it("formats a known timestamp as YYYY-MM-DD HH:mm", () => {
    const ts = new Date("2025-01-15T14:30:00").getTime();
    expect(formatFullTime(ts)).toBe("2025-01-15 14:30");
  });

  it("zero-pads single-digit month, day, hour, and minute", () => {
    const ts = new Date("2025-03-05T04:07:00").getTime();
    expect(formatFullTime(ts)).toBe("2025-03-05 04:07");
  });

  it("handles end-of-year timestamps", () => {
    const ts = new Date("2025-12-31T23:59:00").getTime();
    expect(formatFullTime(ts)).toBe("2025-12-31 23:59");
  });
});

// ── formatDate ──────────────────────────────────────────────────────
describe("formatDate", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2025-06-15T10:30:00"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns i18n today for today', () => {
    const ts = new Date("2025-06-15T08:00:00").getTime();
    expect(formatDate(ts, t_zh)).toBe("今天");
    expect(formatDate(ts, t_en)).toBe("Today");
  });

  it('returns i18n yesterday for yesterday', () => {
    const ts = new Date("2025-06-14T22:00:00").getTime();
    expect(formatDate(ts, t_zh)).toBe("昨天");
    expect(formatDate(ts, t_en)).toBe("Yesterday");
  });

  it("returns YYYY-MM-DD for any other date", () => {
    const ts = new Date("2025-06-10T10:00:00").getTime();
    expect(formatDate(ts, t_zh)).toBe("2025-06-10");
  });

  it("returns YYYY-MM-DD for a date in a different year", () => {
    const ts = new Date("2023-01-01T00:00:00").getTime();
    expect(formatDate(ts, t_zh)).toBe("2023-01-01");
  });
});

describe("plainTextPreview", () => {
  it("strips markdown markers and table pipes", () => {
    expect(plainTextPreview("**bold** and `code` | a | b |")).toBe("bold and code a b");
  });

  it("keeps link text and truncates with an ellipsis", () => {
    expect(plainTextPreview("[docs](https://example.com) here", 20)).toBe("docs here");
    expect(plainTextPreview("abcdefghij", 5)).toBe("abcd…");
  });
});

