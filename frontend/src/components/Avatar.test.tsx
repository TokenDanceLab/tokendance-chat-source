import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { Avatar } from "./Avatar";

vi.mock("@/i18n/context", () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = { "a11y.online": "在线" };
      return map[key] ?? key;
    },
    lang: "zh-CN" as const,
    setLang: vi.fn(),
  }),
}));

describe("Avatar", () => {
  it("renders with username initial letter", () => {
    render(<Avatar name="TestUser" />);
    expect(screen.getByText("T")).toBeInTheDocument();
  });

  it("renders fallback initial when name is empty", () => {
    render(<Avatar name="" />);
    expect(screen.getByText("?")).toBeInTheDocument();
  });

  it("renders with image URL when provided", () => {
    render(
      <Avatar name="TestUser" src="https://example.com/avatar.jpg" />,
    );
    const img = screen.getByRole("img");
    expect(img).toHaveAttribute("src", "https://example.com/avatar.jpg");
    expect(img).toHaveAttribute("alt", "TestUser");
  });

  it("falls back to initials when the image fails", () => {
    render(
      <Avatar name="TestUser" src="https://example.com/missing-avatar.jpg" />,
    );

    fireEvent.error(screen.getByRole("img"));

    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    expect(screen.getByText("T")).toBeInTheDocument();
  });

  it("shows online status indicator when online", () => {
    render(<Avatar name="TestUser" online />);
    const dot = screen.getByRole("status");
    expect(dot).toHaveAttribute("aria-label", "在线");
  });

  it("does not show online indicator when offline", () => {
    render(<Avatar name="TestUser" online={false} />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("does not show online indicator when online prop is omitted", () => {
    render(<Avatar name="TestUser" />);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("renders with accessible label from name", () => {
    render(<Avatar name="TestUser" />);
    const container = screen.getByLabelText("TestUser");
    expect(container).toBeInTheDocument();
  });

  it("applies tokenized identity background to initials", () => {
    render(<Avatar name="TestUser" />);
    const initials = screen.getByText("T");
    expect(initials.style.background).toContain("var(--chat-identity-avatar)");
    expect(initials.style.getPropertyValue("--chat-identity-hue")).not.toBe("");
  });
});
