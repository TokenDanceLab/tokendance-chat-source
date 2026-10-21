import { beforeEach, describe, expect, it } from "vitest";
import { render, screen, within, fireEvent } from "@testing-library/react";
import { I18nProvider } from "@/i18n/context";
import { AssistantParts } from "./message-parts";
import type { ChatPart } from "@/lib/api";

const sources: ChatPart[] = [
  { type: "source", source_id: "1", title: "Release notes", url: "https://example.com/release", status: "fetched", cited: true },
  { type: "source", source_id: "2", title: "Candidate", url: "https://example.org/candidate", status: "discovered", cited: true },
  { type: "source", source_id: "3", title: "Unavailable", url: "https://example.net/down", status: "failed", cited: true },
];
beforeEach(() => localStorage.clear());

function show(parts: ChatPart[], streaming = false, publicOnly = true) {
  return render(<I18nProvider><AssistantParts parts={parts} streaming={streaming} publicOnly={publicOnly} renderText={(text) => <p>{text}</p>} /></I18nProvider>);
}

describe("web source presentation", () => {
  it.each([
    ["zh-CN", "网页来源", "已读取", "仅发现 · 未读取", "读取失败", "已引用", "在新标签页打开"],
    ["en-US", "Web sources", "Read", "Discovered · Not read", "Could not read", "Cited", "Open in a new tab"],
  ])("shows read/discovery/failure and accessible external links in %s", (lang, title, read, found, failed, cited, open) => {
    localStorage.setItem("tokendance:lang", lang);
    show([...sources, sources[0]]);
    const list = screen.getByRole("region", { name: title });
    expect(within(list).getAllByRole("link")).toHaveLength(3);
    expect(within(list).getByText(read)).toBeInTheDocument();
    expect(within(list).getByText(found)).toBeInTheDocument();
    expect(within(list).getByText(failed)).toBeInTheDocument();
    expect(within(list).getAllByText(cited)).toHaveLength(1);
    const link = within(list).getByRole("link", { name: `Release notes · ${read} · ${cited} · ${open}` });
    expect(link).toHaveAttribute("href", "https://example.com/release");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(link).toHaveAttribute("target", "_blank");
    link.focus();
    expect(link).toHaveFocus();
  });

  it("renders hostile titles as text, excludes unsafe sources, and hides raw web cards", () => {
    const { container } = show([
      ...sources, { ...sources[0], source_id: "unsafe", url: "javascript:alert(1)", title: "Unsafe link" },
      { ...sources[0], source_id: "title", url: "https://example.com/title", title: "<img src=x onerror=alert(1)>" },
      { type: "tool-call", tool_name: "web_search", arguments: "PRIVATE QUERY", status: "running" },
      { type: "tool-call", tool_name: "web_fetch", result: "PRIVATE PAGE", status: "running" },
    ], false, false);
    expect(screen.queryByText(/PRIVATE QUERY|PRIVATE PAGE|Unsafe link/)).not.toBeInTheDocument();
    // The fourth unique source sits behind the "show more" toggle.
    fireEvent.click(screen.getByRole("button", { name: /显示其余|Show .* more sources/ }));
    expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
    expect(container.querySelector("img")).toBeNull();
  });

  it("does not infer sources from answer links or show sources on drafts", () => {
    const { unmount } = show([{ type: "text", text: "[Ordinary link](https://example.com)" }]);
    expect(screen.queryByRole("region")).toBeNull();
    unmount();
    show(sources, true);
    expect(screen.queryByRole("region")).toBeNull();
  });
});
