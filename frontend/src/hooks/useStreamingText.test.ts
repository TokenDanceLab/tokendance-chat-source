import { describe, expect, it } from "vitest";
import {
  initialStreamedText,
  nextStreamingTextBoundary,
  STREAMING_TEXT_TIMING,
} from "@/hooks/useStreamingText";

describe("streaming text reveal", () => {
  it("reveals Unicode words as atomic boundaries", () => {
    expect(nextStreamingTextBoundary("Hello world", 0)).toBe(5);
    expect(nextStreamingTextBoundary("你好世界", 0)).toBeGreaterThan(0);
    expect(nextStreamingTextBoundary("你好世界", 0)).toBeLessThanOrEqual(4);
  });

  it("seeds a streaming part with one word instead of an empty bubble", () => {
    expect(initialStreamedText("Hello world", true)).toBe("Hello");
    expect(initialStreamedText("finished", false)).toBe("finished");
    expect(initialStreamedText("", true)).toBe("");
  });

  it("keeps Muse's pacing constants explicit and reviewable", () => {
    expect(STREAMING_TEXT_TIMING).toMatchObject({
      streamingLagMs: 400,
      settlingLagMs: 150,
      minimumCharsPerSecond: 72,
      maximumAccelerationCharsPerSecondSquared: 18_000,
      settlingDeadlineMs: 1_500,
      maximumFrameDeltaMs: 100,
      streamWordFadeMs: 150,
    });
  });
});
