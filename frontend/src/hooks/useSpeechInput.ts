import { useCallback, useEffect, useRef, useState } from "react";

interface SpeechResultLike {
  0?: { transcript?: string };
}
interface SpeechEventLike {
  results?: ArrayLike<SpeechResultLike>;
}
interface SpeechRecognitionLike {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  onresult: ((event: SpeechEventLike) => void) | null;
  onerror: (() => void) | null;
  onend: (() => void) | null;
  start: () => void;
  stop: () => void;
  abort: () => void;
}
type SpeechRecognitionCtor = new () => SpeechRecognitionLike;

function recognitionCtor(): SpeechRecognitionCtor | null {
  if (typeof window === "undefined") return null;
  const candidate = window as unknown as {
    SpeechRecognition?: SpeechRecognitionCtor;
    webkitSpeechRecognition?: SpeechRecognitionCtor;
  };
  return candidate.SpeechRecognition ?? candidate.webkitSpeechRecognition ?? null;
}

/**
 * Browser-native dictation. No audio leaves the browser through our server;
 * unsupported browsers simply do not render the microphone entry.
 */
export function useSpeechInput(
  onText: (text: string) => void,
  lang: string,
  onError?: () => void,
) {
  const [supported] = useState(() => recognitionCtor() !== null);
  const [listening, setListening] = useState(false);
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null);
  const textRef = useRef(onText);
  const errorRef = useRef(onError);
  textRef.current = onText;
  errorRef.current = onError;

  const stop = useCallback(() => {
    try {
      recognitionRef.current?.stop();
    } catch {
      /* already stopped */
    }
    setListening(false);
  }, []);

  const start = useCallback(() => {
    const Ctor = recognitionCtor();
    if (!Ctor) return;
    try {
      recognitionRef.current?.abort();
    } catch {
      /* already stopped */
    }
    const recognition = new Ctor();
    recognition.continuous = false;
    recognition.interimResults = false;
    recognition.lang = lang === "en-US" ? "en-US" : "zh-CN";
    recognition.onresult = (event) => {
      let text = "";
      const results = event.results;
      if (results) {
        for (let index = 0; index < results.length; index++) {
          text += results[index]?.[0]?.transcript ?? "";
        }
      }
      text = text.trim();
      if (text) textRef.current(text);
    };
    recognition.onerror = () => {
      setListening(false);
      errorRef.current?.();
    };
    recognition.onend = () => setListening(false);
    recognitionRef.current = recognition;
    try {
      recognition.start();
      setListening(true);
    } catch {
      setListening(false);
      errorRef.current?.();
    }
  }, [lang]);

  useEffect(
    () => () => {
      try {
        recognitionRef.current?.abort();
      } catch {
        /* already stopped */
      }
    },
    [],
  );

  return { supported, listening, start, stop };
}
