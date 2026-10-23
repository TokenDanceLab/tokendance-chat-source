/**
 * Browser storage keys. Keep the string values stable: changing one silently
 * logs users out or drops their preferences.
 */
export const STORAGE_KEYS = {
  auth: "tokendance:auth",
  username: "tokendance:username",
  guestName: "tokendance:guestName",
  legacySessionToken: "tokendance:sessionToken",
  language: "tokendance:lang",
  soundEnabled: "tokendance:soundEnabled",
  theme: "tdchat-theme",
  emojiRecents: "tdchat:recent-emojis",
  lastReadPrefix: "tokendance:lastReadTimestamps",
} as const;
