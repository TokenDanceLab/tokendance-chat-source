import { StrictMode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@/i18n/context";

const wsMocks = vi.hoisted(() => ({
  connect: vi.fn(),
}));

// Mock WebSocket before any imports that use it
vi.mock("@/hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connect: wsMocks.connect,
    disconnect: vi.fn(),
    sendMessage: vi.fn(),
    markRead: vi.fn(),
    sendReaction: vi.fn(),
    sendMessageEdit: vi.fn(),
  }),
}));

// Mock sound config module
vi.mock("@/lib/soundToggle", () => ({
  isSoundEnabled: vi.fn(() => true),
  setSoundEnabled: vi.fn(),
}));

// Mock sound module — AudioContext not available in jsdom
vi.mock("@/lib/sound", () => ({
  playMessageSound: vi.fn(),
  playMentionSound: vi.fn(),
  playOnlineSound: vi.fn(),
  playOfflineSound: vi.fn(),
  playSentSound: vi.fn(),
  playReactionSound: vi.fn(),
}));

// Server config is fetched from /api/config. This suite tests guest auto-join
// behavior, not config loading, so stub the hook to avoid an extra fetch that
// would otherwise trip the mockFetch assertions below.
vi.mock("@/hooks/useServerConfig", () => ({
  useServerConfig: () => null,
}));

const mockFetch = vi.fn();
globalThis.fetch = mockFetch;

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value; },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { store = {}; },
  };
})();
Object.defineProperty(window, "localStorage", { value: localStorageMock });

// Mock matchMedia (used by theme init)
Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
});

Element.prototype.scrollTo = vi.fn();

// Must import App AFTER all mocks are set up
import App from "@/App";
import { JoinError } from "@/lib/api";
import { useChatStore } from "@/stores/chatStore";

describe("App smoke test", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/");
    localStorageMock.clear();
    sessionStorage.clear();
    localStorageMock.setItem("tokendance:lang", "zh-CN");
    wsMocks.connect.mockReset();
    wsMocks.connect.mockResolvedValue(undefined);
    mockFetch.mockReset();
    mockFetch.mockRejectedValue(new Error("no backend"));
    useChatStore.getState().reset();
  });

  it("renders ChatLayout without crashing", () => {
    expect(() => {
      render(
        <I18nProvider>
          <App />
        </I18nProvider>,
      );
    }).not.toThrow();
  });

  it("renders the chat interface by default", () => {
    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );
    // Chat input placeholder should be visible while the app auto-joins guest mode.
    expect(screen.getByPlaceholderText(/输入消息/)).toBeTruthy();
  });

  it("auto-joins a guest user instead of leaving the composer disabled", async () => {
    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );

    await waitFor(() => {
      expect(wsMocks.connect).toHaveBeenCalled();
    });
    const guestName = wsMocks.connect.mock.calls[0][0] as string;
    expect(guestName).toMatch(/^guest_[a-z0-9]{8}$/);
    expect(useChatStore.getState().username).toBe(guestName);
    expect(useChatStore.getState().isGuest).toBe(true);
    expect(mockFetch).toHaveBeenCalledWith("/api/session", {
      cache: "no-store",
      credentials: "same-origin",
    });
  });

  it("auto-joins guest mode when React StrictMode re-runs effects", async () => {
    render(
      <StrictMode>
        <I18nProvider>
          <App />
        </I18nProvider>
      </StrictMode>,
    );

    await waitFor(() => {
      expect(wsMocks.connect).toHaveBeenCalled();
    });
    expect((wsMocks.connect.mock.calls[0][0] as string)).toMatch(/^guest_[a-z0-9]{8}$/);
  });

  it("reuses the stored guest hint when the session has no bound handle", async () => {
    localStorageMock.setItem("tokendance:guestName", "guest_stored");
    render(<I18nProvider><App /></I18nProvider>);
    await waitFor(() => expect(wsMocks.connect).toHaveBeenCalledWith("guest_stored"));
    expect(screen.queryByText(/游客模式.*用户名/)).toBeNull();
  });

  it("adopts the guest handle bound to this browser's cookie", async () => {
    localStorageMock.setItem("tokendance:guestName", "guest_stale");
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const url = input.toString();
      if (url === "/api/session") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ authenticated: false, guest: true, guest_username: "guest_bound" }),
        } as unknown as Response);
      }
      return Promise.reject(new Error("no backend"));
    });
    render(<I18nProvider><App /></I18nProvider>);
    await waitFor(() => expect(wsMocks.connect).toHaveBeenCalledWith("guest_bound"));
    expect(localStorageMock.getItem("tokendance:guestName")).toBe("guest_bound");
  });

  it("retries with the bound guest handle after an identity mismatch", async () => {
    localStorageMock.setItem("tokendance:guestName", "guest_stale");
    let sessionCalls = 0;
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const url = input.toString();
      if (url === "/api/session") {
        sessionCalls += 1;
        const bound = sessionCalls > 1;
        return Promise.resolve({
          ok: true,
          json: async () => (
            bound
              ? { authenticated: false, guest: true, guest_username: "guest_bound" }
              : { authenticated: false, guest: true }
          ),
        } as unknown as Response);
      }
      return Promise.reject(new Error("no backend"));
    });
    wsMocks.connect
      .mockRejectedValueOnce(new JoinError("GUEST_IDENTITY_MISMATCH", "this browser is guest_bound"))
      .mockResolvedValueOnce(undefined);

    render(<I18nProvider><App /></I18nProvider>);

    await waitFor(() => expect(wsMocks.connect).toHaveBeenLastCalledWith("guest_bound"));
    expect(localStorageMock.getItem("tokendance:guestName")).toBe("guest_bound");
  });

  it("falls back to public preview when guest WebSocket join fails", async () => {
    wsMocks.connect.mockRejectedValueOnce(new Error("offline"));
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const url = input.toString();
      if (url === "/api/session") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ authenticated: false }),
        } as unknown as Response);
      }
      if (url === "/api/messages?limit=100") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            messages: [
              {
                id: "preview-1",
                username: "TokenBot",
                content: "Preview welcome",
                timestamp: 1000,
              },
            ],
          }),
        } as unknown as Response);
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });

    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith("/api/messages?limit=100", { redirect: "manual" });
    });
    expect(await screen.findByText("Preview welcome")).toBeTruthy();
    expect(useChatStore.getState().username).toBe("");
    expect(useChatStore.getState().isGuest).toBe(false);
  });

  it("clears stale username-only storage without leaving the auth modal over guest chat", async () => {
    localStorageMock.setItem("tokendance:username", "stale_guest");

    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );

    await waitFor(() => {
      expect(wsMocks.connect).toHaveBeenCalled();
    });

    const guestName = wsMocks.connect.mock.calls[0][0] as string;
    expect(guestName).toMatch(/^guest_[a-z0-9]{8}$/);
    expect(useChatStore.getState().username).toBe(guestName);
    expect(useChatStore.getState().isGuest).toBe(true);
    expect(useChatStore.getState().showAuthModal).toBe(false);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(window.localStorage.getItem("tokendance:username")).toBeNull();
  });

  it("connects from the HttpOnly session after an OIDC callback", async () => {
    window.history.replaceState({}, "", "/?oidc_success=1");
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const url = input.toString();
      if (url === "/api/session") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            authenticated: true,
            username: "alice",
            kind: "oidc",
          }),
        } as unknown as Response);
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });

    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith("/api/session", {
        cache: "no-store",
        credentials: "same-origin",
      });
    });
    await waitFor(() => {
      expect(wsMocks.connect).toHaveBeenCalledWith("alice");
    });
    expect(window.localStorage.getItem("tokendance:username")).toBe("alice");
    expect(window.localStorage.getItem("tokendance:auth")).toBe("true");
    expect(window.localStorage.getItem("tokendance:sessionToken")).toBeNull();
    expect(window.location.search).toBe("");
  });

  it("returns to guest mode after Account global logout without keeping the marker URL", async () => {
    window.history.replaceState({}, "", "/?oidc_logged_out=1");
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const url = input.toString();
      if (url === "/api/session") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ authenticated: false }),
        } as unknown as Response);
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });

    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );

    await waitFor(() => {
      expect(wsMocks.connect).toHaveBeenCalled();
    });
    expect(window.location.search).toBe("");
    expect(useChatStore.getState().isGuest).toBe(true);
    expect(useChatStore.getState().showAuthModal).toBe(false);
  });

  it("clears a rejected Account logout callback marker and stays signed out", async () => {
    window.history.replaceState({}, "", "/?oidc_logout_error=1");
    mockFetch.mockImplementation((input: RequestInfo | URL) => {
      const url = input.toString();
      if (url === "/api/session") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ authenticated: false }),
        } as unknown as Response);
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });

    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );

    await waitFor(() => {
      expect(wsMocks.connect).toHaveBeenCalled();
    });
    expect(window.location.search).toBe("");
    expect(useChatStore.getState().isGuest).toBe(true);
    expect(useChatStore.getState().showAuthModal).toBe(false);
  });
});
