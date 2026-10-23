import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useSpeechInput } from "@/hooks/useSpeechInput";

type Handler = ((event: unknown) => void) | null;

class FakeRecognition {
  continuous = false;
  interimResults = false;
  lang = "";
  onresult: Handler = null;
  onerror: Handler = null;
  onend: (() => void) | null = null;
  start = vi.fn();
  stop = vi.fn();
  abort = vi.fn();
}

afterEach(() => {
  delete (window as unknown as { SpeechRecognition?: unknown }).SpeechRecognition;
  delete (window as unknown as { webkitSpeechRecognition?: unknown }).webkitSpeechRecognition;
});

describe("useSpeechInput", () => {
  it("reports unsupported when the browser has no recognition API", () => {
    const { result } = renderHook(() => useSpeechInput(vi.fn(), "zh-CN"));
    expect(result.current.supported).toBe(false);
  });

  it("starts recognition and forwards the transcript", () => {
    const instances: FakeRecognition[] = [];
    class TrackedRecognition extends FakeRecognition {
      constructor() {
        super();
        instances.push(this);
      }
    }
    (window as unknown as { SpeechRecognition: unknown }).SpeechRecognition = TrackedRecognition;
    const onText = vi.fn();
    const { result } = renderHook(() => useSpeechInput(onText, "en-US"));

    act(() => result.current.start());
    expect(result.current.supported).toBe(true);
    expect(result.current.listening).toBe(true);
    expect(instances).toHaveLength(1);
    expect(instances[0].lang).toBe("en-US");
    expect(instances[0].start).toHaveBeenCalledOnce();

    act(() => {
      instances[0].onresult?.({ results: [[{ transcript: "hello " }], [{ transcript: "world" }]] });
      instances[0].onend?.();
    });
    expect(onText).toHaveBeenCalledWith("hello world");
    expect(result.current.listening).toBe(false);
  });

  it("reports an error when start throws", () => {
    class BrokenRecognition extends FakeRecognition {
      start = vi.fn(() => {
        throw new Error("denied");
      });
    }
    (window as unknown as { SpeechRecognition: unknown }).SpeechRecognition = BrokenRecognition;
    const onError = vi.fn();
    const { result } = renderHook(() => useSpeechInput(vi.fn(), "zh-CN", onError));

    act(() => result.current.start());
    expect(result.current.listening).toBe(false);
    expect(onError).toHaveBeenCalledOnce();
  });
});
