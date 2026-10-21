import { useEffect, useState } from "react";

/**
 * Keeps the last connected state for a short grace period. A one-off socket
 * blip or a fast server restart should not flash "connecting" in the chrome;
 * gates that prevent sending still use the real `connected` value.
 */
export function useConnectionSettled(connected: boolean, delayMs = 1500): boolean {
  const [settled, setSettled] = useState(connected);
  useEffect(() => {
    if (connected) {
      setSettled(true);
      return;
    }
    const timer = setTimeout(() => setSettled(false), delayMs);
    return () => clearTimeout(timer);
  }, [connected, delayMs]);
  return settled;
}
