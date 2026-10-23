import { describe, it, expect } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { AssistantIcon } from "@/components/AssistantIcon";
import type { AssistantDefinition, AssistantModel } from "@/lib/assistantRegistry";

const mockModel: AssistantModel = {
  id: "deepseek-v4-flash",
  name: "DeepSeek V4 Flash",
  provider: "deepseek",
  providerName: "DeepSeek",
  protocol: "openai",
  icon: "deepseek",
};

const mockUnknownModel: AssistantModel = {
  id: "unknown-model",
  name: "Unknown",
  provider: "unknown",
  providerName: "Unknown",
  protocol: "openai",
  icon: "unknown_provider",
};

const botWithoutIcon: AssistantDefinition = {
  id: "testbot",
  name: "TestBot",
  label: "Bot",
  description: "A test bot without an icon model",
  kind: "bot",
  aliases: [],
  mention: "@TestBot",
  model: { ...mockModel, icon: "" },
  status: "online",
};

describe("AssistantIcon", () => {
  it("uses the configured assistant image independent of model and recovers through provider fallback", () => {
    const assistant = { ...botWithoutIcon, model: mockModel, avatarUrl: "/bot-identity.png" };
    const { container, rerender } = render(<AssistantIcon assistant={assistant} avatarUrl="/live-bot.png" />);
    expect(screen.getByRole("img")).toHaveAttribute("src", "/live-bot.png");
    fireEvent.error(screen.getByRole("img"));
    expect(screen.getByRole("img")).toHaveAttribute("src", "/bot-identity.png");
    fireEvent.error(screen.getByRole("img"));
    expect(screen.queryByRole("img")).toBeNull();
    expect(container.querySelector("svg")).toBeTruthy();
    rerender(<AssistantIcon assistant={{ ...assistant, avatarUrl: "/new-identity.png" }} />);
    expect(screen.getByRole("img")).toHaveAttribute("src", "/new-identity.png");
  });
  describe("rendering", () => {
    it("model 模式下渲染 ProviderColorIcon SVG（非 img）", () => {
      const { container } = render(<AssistantIcon model={mockModel} size="md" />);
      // Known providers render inline SVG, not <img>.
      expect(screen.queryByRole("img")).toBeNull();
      // The SVG wrapper should be present.
      const svg = container.querySelector("svg");
      expect(svg).toBeTruthy();
    });

    it("assistant 模式下通过其 model 渲染 ProviderColorIcon", () => {
      const assistant: AssistantDefinition = {
        id: "test",
        name: "Test",
        label: "Bot",
        description: "",
        kind: "bot",
        aliases: [],
        mention: "@Test",
        model: mockModel,
        status: "online",
      };
      const { container } = render(<AssistantIcon assistant={assistant} size="md" />);
      expect(screen.queryByRole("img")).toBeNull();
      expect(container.querySelector("svg")).toBeTruthy();
    });

    it("没有 icon 属性时不渲染 SVG，直接显示 fallback（Bot icon）", () => {
      render(<AssistantIcon assistant={botWithoutIcon} size="md" />);
      expect(screen.queryByRole("img")).toBeNull();
      expect(document.querySelector("svg")).toBeTruthy(); // lucide icon
    });

    it("bot 类型没有 icon 时显示 Bot 图标作为 fallback", () => {
      const { container } = render(<AssistantIcon assistant={botWithoutIcon} size="md" />);
      expect(container.querySelector("svg")).toBeTruthy();
    });

    it("没有 model 和 assistant 时显示 fallback", () => {
      const { container } = render(<AssistantIcon size="md" />);
      expect(container.querySelector("svg")).toBeTruthy();
    });

    it("未知 provider 显示 lucide fallback 而非 SVG", () => {
      render(<AssistantIcon model={mockUnknownModel} size="md" />);
      // Unknown provider: falls back to lucide Bot icon
      expect(document.querySelector("svg")).toBeTruthy();
    });
  });
});
