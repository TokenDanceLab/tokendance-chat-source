import { useEffect, useRef, useState } from "react";

/**
 * Keep rendered streamed text slightly behind the network target. The cost of
 * a Markdown parse is then paid at a bounded cadence instead of once per token.
 */
export const STREAMING_TEXT_TIMING = {
  averageWordChars: 6,
  streamingLagMs: 400,
  settlingLagMs: 150,
  minimumCharsPerSecond: 72,
  maximumAccelerationCharsPerSecondSquared: 18_000,
  settlingDeadlineMs: 1_500,
  minimumPaintIntervalMs: 32,
  maximumFrameDeltaMs: 100,
  streamWordFadeMs: 150,
} as const;

let wordSegmenter: Intl.Segmenter | null | undefined;
let segmentCache: { text: string; segments: Intl.Segments } | null = null;

function segmentsFor(text: string): Intl.Segments | null {
  if (typeof Intl === "undefined" || typeof Intl.Segmenter !== "function") return null;
  if (segmentCache?.text === text) return segmentCache.segments;
  wordSegmenter ??= new Intl.Segmenter(undefined, { granularity: "word" });
  const segments = wordSegmenter.segment(text);
  segmentCache = { text, segments };
  return segments;
}

/**
 * The next atomic reveal boundary. Unicode word segmentation keeps CJK text
 * from advancing one UTF-16 code unit at a time.
 */
export function nextStreamingTextBoundary(text: string, fromIndex: number): number {
  if (fromIndex >= text.length) return text.length;
  const segments = segmentsFor(text);
  if (!segments) {
    const whitespace = text.indexOf(" ", fromIndex);
    const newline = text.indexOf("\n", fromIndex);
    const boundaries = [whitespace, newline].filter((index) => index >= 0);
    return boundaries.length ? Math.min(...boundaries) + 1 : text.length;
  }

  for (const segment of segments) {
    const end = segment.index + segment.segment.length;
    if (end <= fromIndex) continue;
    return end;
  }
  return text.length;
}

export function initialStreamedText(text: string, isStreaming: boolean): string {
  if (!isStreaming || !text) return isStreaming ? "" : text;
  return text.slice(0, nextStreamingTextBoundary(text, 0));
}

export function useStreamingText(
  text: string,
  isStreaming: boolean,
): { displayedText: string; isAnimating: boolean; isFullyComplete: boolean } {
  const [displayedText, setDisplayedText] = useState(() =>
    initialStreamedText(text, isStreaming),
  );
  const [isAnimating, setIsAnimating] = useState(false);
  const [isFullyComplete, setIsFullyComplete] = useState(() => !isStreaming);
  const targetRef = useRef(text);
  const displayedLengthRef = useRef(displayedText.length);
  const streamingRef = useRef(isStreaming);
  const frameRef = useRef<number | null>(null);
  const completionTimerRef = useRef<number | null>(null);
  const lastFrameAtRef = useRef(0);
  const lastPaintAtRef = useRef(0);
  const rateRef = useRef<number>(STREAMING_TEXT_TIMING.minimumCharsPerSecond);
  const budgetRef = useRef(0);
  const settledAtRef = useRef<number | null>(null);

  useEffect(() => {
    const previousStreaming = streamingRef.current;
    const target = text;
    targetRef.current = target;
    streamingRef.current = isStreaming;

    const clearCompletionTimer = () => {
      if (completionTimerRef.current != null) {
        window.clearTimeout(completionTimerRef.current);
        completionTimerRef.current = null;
      }
    };
    const scheduleFullyComplete = () => {
      clearCompletionTimer();
      setIsAnimating(false);
      completionTimerRef.current = window.setTimeout(() => {
        completionTimerRef.current = null;
        setIsFullyComplete(true);
      }, STREAMING_TEXT_TIMING.streamWordFadeMs);
    };

    const reduceMotion =
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const snap = (completeImmediately = true) => {
      if (frameRef.current != null) {
        cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
      clearCompletionTimer();
      displayedLengthRef.current = target.length;
      setDisplayedText(target);
      setIsAnimating(false);
      setIsFullyComplete(completeImmediately);
      settledAtRef.current = null;
      budgetRef.current = 0;
    };

    if (
      reduceMotion ||
      (typeof document !== "undefined" && document.hidden) ||
      target.length < displayedLengthRef.current ||
      (!isStreaming && !previousStreaming)
    ) {
      snap();
      return;
    }

    setIsFullyComplete(false);
    clearCompletionTimer();

    if (!isStreaming && previousStreaming) {
      settledAtRef.current = performance.now();
    }

    if (
      !isStreaming &&
      previousStreaming &&
      displayedLengthRef.current >= target.length
    ) {
      scheduleFullyComplete();
      return;
    }

    if (frameRef.current == null && displayedLengthRef.current < target.length) {
      rateRef.current = STREAMING_TEXT_TIMING.minimumCharsPerSecond;
      budgetRef.current = 0;
      lastFrameAtRef.current = performance.now();
      setIsAnimating(true);

      const tick = (timestamp: number) => {
        const currentTarget = targetRef.current;
        const currentLength = displayedLengthRef.current;

        if (currentLength >= currentTarget.length) {
          frameRef.current = null;
          if (!streamingRef.current) scheduleFullyComplete();
          return;
        }

        const settledAt = settledAtRef.current;
        if (
          settledAt != null &&
          timestamp - settledAt > STREAMING_TEXT_TIMING.settlingDeadlineMs
        ) {
          snap();
          return;
        }

        if (
          typeof document !== "undefined" &&
          document.hidden
        ) {
          snap();
          return;
        }

        const elapsedSeconds =
          Math.min(
            STREAMING_TEXT_TIMING.maximumFrameDeltaMs,
            Math.max(0, timestamp - lastFrameAtRef.current),
          ) / 1000;
        lastFrameAtRef.current = timestamp;
        if (timestamp - lastPaintAtRef.current < STREAMING_TEXT_TIMING.minimumPaintIntervalMs) {
          frameRef.current = requestAnimationFrame(tick);
          return;
        }

        const pendingChars = currentTarget.length - currentLength;
        const lagMs = streamingRef.current
          ? STREAMING_TEXT_TIMING.streamingLagMs
          : STREAMING_TEXT_TIMING.settlingLagMs;
        const desiredRate = Math.max(
          STREAMING_TEXT_TIMING.minimumCharsPerSecond,
          pendingChars / (lagMs / 1000),
        );
        const rate = streamingRef.current
          ? Math.min(
              desiredRate,
              rateRef.current +
                STREAMING_TEXT_TIMING.maximumAccelerationCharsPerSecondSquared *
                  elapsedSeconds,
            )
          : desiredRate;
        rateRef.current = rate;
        budgetRef.current = Math.min(
          budgetRef.current + rate * elapsedSeconds,
          pendingChars,
        );

        let nextLength = currentLength;
        while (nextLength < currentTarget.length) {
          const boundary = nextStreamingTextBoundary(currentTarget, nextLength);
          const consumed = boundary - nextLength;
          if (consumed <= 0 || budgetRef.current < consumed) break;
          budgetRef.current -= consumed;
          nextLength = boundary;
        }

        if (nextLength > currentLength) {
          displayedLengthRef.current = nextLength;
          lastPaintAtRef.current = timestamp;
          setDisplayedText(currentTarget.slice(0, nextLength));
        }
        frameRef.current = requestAnimationFrame(tick);
      };

      frameRef.current = requestAnimationFrame(tick);
    }
  }, [isStreaming, text]);

  useEffect(
    () => () => {
      if (frameRef.current != null) cancelAnimationFrame(frameRef.current);
      if (completionTimerRef.current != null) {
        window.clearTimeout(completionTimerRef.current);
      }
    },
    [],
  );

  return { displayedText, isAnimating, isFullyComplete };
}
