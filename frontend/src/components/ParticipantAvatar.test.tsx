import { beforeEach, describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { ParticipantAvatar } from "@/components/ParticipantAvatar";
import { useChatStore } from "@/stores/chatStore";
import { I18nProvider } from "@/i18n/context";

beforeEach(() => useChatStore.getState().reset());
describe("Participant identity resolver", () => {
  it("uses a live bot profile override across every participant surface", () => {
    useChatStore.getState().setUserProfile({ username: "TokenBot", display_name: "TokenBot", avatar_url: "/bot-profile.png", bio: "", status: "online", last_seen: 1, created_at: 1 });
    const { container } = render(<><ParticipantAvatar username="TokenBot" size="sm" /><ParticipantAvatar username="TokenBot" size="lg" /></>);
    expect([...container.querySelectorAll("img")].map((image) => image.getAttribute("src"))).toEqual(["/bot-profile.png", "/bot-profile.png"]);
  });
  it("keeps guest graphics the same for different sizes and display names", () => {
    const { container } = render(<I18nProvider><ParticipantAvatar username="guest_stable" name="Guest" size="sm" /><ParticipantAvatar username="guest_stable" name="Visitor" size="lg" /></I18nProvider>);
    const graphics = container.querySelectorAll('[data-visual="guest-avatar"]');
    expect(graphics).toHaveLength(2);
    expect(graphics[0].innerHTML).toBe(graphics[1].innerHTML);
    expect(graphics[0].getAttribute("data-avatar-seed")).toBe(graphics[1].getAttribute("data-avatar-seed"));
  });
});
