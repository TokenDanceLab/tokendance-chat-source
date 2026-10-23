// SPDX-License-Identifier: AGPL-3.0-only

// Official image builds pin this URL to their source commit. Forks should set
// VITE_SOURCE_URL to their own complete corresponding source before building.
export const sourceUrl =
  import.meta.env.VITE_SOURCE_URL || "https://github.com/TokenDanceLab/tokendance-chat-source";
