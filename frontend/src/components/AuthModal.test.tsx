import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@/i18n/context";
import { useChatStore } from "@/stores/chatStore";
import { AuthModal } from "@/components/AuthModal";
import { loginUser } from "@/lib/api";

const mocks = vi.hoisted(() => ({
  connect: vi.fn(),
  logout: vi.fn(),
  accountOnly: false,
}));

vi.mock("@/hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connect: mocks.connect,
  }),
}));

vi.mock("@/hooks/useServerConfig", () => ({
  useServerConfig: () => ({ oidc_enabled: mocks.accountOnly }),
}));

vi.mock("@/lib/api", () => ({
  loginUser: vi.fn(),
  registerUser: vi.fn(),
  logout: mocks.logout,
  ChatError: class ChatError extends Error {
    code: string;
    constructor(code: string, message: string) {
      super(message);
      this.code = code;
    }
  },
  ErrorCode: {
    TIMEOUT: "ERR_TIMEOUT",
    CLOSED: "ERR_CLOSED",
    CANNOT_CONNECT: "ERR_CANNOT_CONNECT",
    AUTH_FAILED: "ERR_AUTH_FAILED",
  },
}));

function renderOpenAuthModal() {
  useChatStore.getState().reset();
  useChatStore.getState().setShowAuthModal(true);
  return render(
    <I18nProvider>
      <AuthModal />
    </I18nProvider>,
  );
}

function openLoginTab() {
  // Radix Tabs activates on pointer down, so a bare click never changes tabs.
  fireEvent.mouseDown(screen.getByRole("tab", { name: "登录" }));
}

describe("AuthModal", () => {
  beforeEach(() => {
    mocks.connect.mockReset();
    mocks.accountOnly = false;
    mocks.logout.mockReset();
    mocks.logout.mockResolvedValue(undefined);
    window.localStorage.clear();
    window.localStorage.setItem("tokendance:lang", "zh-CN");
  });

  it("renders as a labelled modal dialog", () => {
    renderOpenAuthModal();

    const dialog = screen.getByRole("dialog", { name: "游客加入" });
    expect(dialog.getAttribute("data-slot")).toBe("dialog-content");
  });

  it("renders the real Account CTA with its primary foreground in OIDC mode", () => {
    mocks.accountOnly = true;
    renderOpenAuthModal();

    const link = screen.getByRole("link", { name: "使用 TokenDance Account 继续" });
    expect(link).toHaveAttribute("href", "/api/oidc/login");
    expect(link).toHaveClass("bg-primary", "text-primary-foreground", "text-name");
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "加入聊天" })).toBeDisabled();
  });

  it("exposes stable visual selectors for browser acceptance", () => {
    renderOpenAuthModal();

    expect(document.querySelector("[data-visual='auth-modal']")).toBeTruthy();
    expect(document.querySelector("[data-visual='auth-modal-tabs']")).toBeTruthy();
    expect(document.querySelectorAll("[data-visual='auth-modal-tab']").length).toBe(3);
    expect(document.querySelector("[data-visual='auth-modal-content']")).toBeTruthy();
  });

  it("labels the guest username input for assistive technology", () => {
    renderOpenAuthModal();

    const input = screen.getByLabelText("你的用户名...");
    expect(input.getAttribute("placeholder")).toBe("你的用户名...");
  });

  it("keeps the password visibility toggle keyboard reachable", () => {
    renderOpenAuthModal();

    openLoginTab();
    const toggle = screen.getByRole("button", { name: "显示密码" });

    expect(toggle.getAttribute("tabindex")).not.toBe("-1");
  });

  it("does not persist auth when credential login succeeds but WebSocket join fails", async () => {
    vi.mocked(loginUser).mockResolvedValueOnce({
      success: true,
      username: "alice",
    });
    mocks.connect.mockRejectedValueOnce(new Error("join failed"));
    renderOpenAuthModal();

    openLoginTab();
    fireEvent.change(screen.getByLabelText("用户名"), {
      target: { value: "alice" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "correct-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登录" }));

    await waitFor(() => {
      expect(screen.getByRole("alert")).toHaveTextContent("join failed");
    });
    expect(mocks.connect).toHaveBeenCalledWith("alice");
    expect(window.localStorage.getItem("tokendance:auth")).toBeNull();
    expect(window.localStorage.getItem("tokendance:username")).toBeNull();
    expect(mocks.logout).toHaveBeenCalledOnce();
  });

  it("persists auth state only after credential login joins WebSocket", async () => {
    vi.mocked(loginUser).mockResolvedValueOnce({
      success: true,
      username: "alice",
    });
    mocks.connect.mockResolvedValueOnce(undefined);
    renderOpenAuthModal();

    openLoginTab();
    fireEvent.change(screen.getByLabelText("用户名"), {
      target: { value: "alice" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "correct-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登录" }));

    await waitFor(() => {
      expect(mocks.connect).toHaveBeenCalledWith("alice");
    });
    expect(window.localStorage.getItem("tokendance:auth")).toBe("true");
    expect(window.localStorage.getItem("tokendance:username")).toBe("alice");
    expect(window.localStorage.getItem("tokendance:sessionToken")).toBeNull();
  });
});
