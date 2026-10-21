import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { OidcLoginButton } from "./OidcLoginButton";
import { mockI18n } from "@/test-utils";

vi.mock("@/i18n/context", () => ({
  useTranslation: () =>
    mockI18n({
      "auth.oidcLoginButton": "Continue with TokenDance Account",
    }),
}));

describe("OidcLoginButton", () => {
  it("preserves the primary foreground together with the custom font size", () => {
    render(<OidcLoginButton primary />);

    const link = screen.getByRole("link", { name: "Continue with TokenDance Account" });
    expect(link).toHaveClass("bg-primary", "text-primary-foreground", "text-name");
    expect(link).not.toHaveClass("text-sm");
    const mark = link.querySelector("[data-brand='tokendance-account']");
    expect(mark).toHaveAttribute("aria-hidden", "true");
    expect(mark).toHaveClass("shrink-0", "bg-current");
    expect(link).toHaveAccessibleName("Continue with TokenDance Account");
  });

  it("renders login link without probing a protected API", () => {
    const fetchMock = vi.spyOn(window, "fetch");
    render(<OidcLoginButton />);

    const link = screen.getByRole("link");
    expect(link.getAttribute("href")).toBe("/api/oidc/login");
    expect(link.getAttribute("data-visual")).toBe("auth-modal-oidc");
    expect(link.className).toContain("min-h-11");
    expect(fetchMock).not.toHaveBeenCalled();
    fetchMock.mockRestore();
  });
});
