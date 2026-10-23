import { create } from "zustand";
import type { ChatMessage, UserStatus, CustomEmoji, PollData, WSBotRunEvent, WSStreamEvent, BotRunStatus, PrivateThreadSummary } from "@/lib/api";
import { CHAT_EVENTS } from "@/lib/events";
import { STORAGE_KEYS } from "@/lib/storage";
import { mergeMessageWindow } from "@/stores/mergeMessageWindow";

import { acceptFinal, clearActiveRuns, finishMessage, isActiveRun, privateMessage, publicMessage, receiveRun, receiveStream, type BotRun } from "@/stores/botRunState";

const MESSAGE_CAP = 500;
const HISTORY_CAP = 1000;

/**
 * Stable chronological order. The server timestamps every persisted message;
 * local optimistic and system rows use Date.now() at insert. Sorting after a
 * history merge keeps a reconnect snapshot or an out-of-order page from
 * appearing above newer live messages.
 */
function sortChronologically(messages: ChatMessage[]): ChatMessage[] {
  return [...messages].sort((a, b) => {
    if (a.timestamp !== b.timestamp) return a.timestamp - b.timestamp;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

function getLSLastReadKey(username: string): string {
  return `${STORAGE_KEYS.lastReadPrefix}:${username}`;
}

function loadLastReadTimestamps(username: string): Record<string, number> {
  try {
    const raw = localStorage.getItem(getLSLastReadKey(username));
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

export type ViewState = "chat";

export type CurrentChat = { type: "public" };

export type LegacyChatInput =
  | CurrentChat
  | { type: "dm"; username: string }
  | { type: "group"; name: string };

export interface MentionNotification {
  from: string;
  content: string;
  messageId: string;
  roomId?: string;
  group?: string;
  timestamp: number;
}

export interface UserProfile {
  username: string;
  display_name: string;
  avatar_url: string;
  bio: string;
  status: string;
  last_seen: number;
  created_at: number;
}

export interface NotificationPref {
  mutedUntil: number;
  showPreview: boolean;
}

interface ChatState {
  // Connection state
  view: ViewState;
  username: string;
  connected: boolean;
  isGuest: boolean;

  // Messages
  messages: ChatMessage[];
  messageWindowRevision: number;
  historyLoaded: boolean;
  // Private 1:1 with the assistant (TokenBot) — separate from the public room.
  // Holds only the active lane; switching lanes reloads via load_private.
  privateBotMessages: ChatMessage[];
  // Muse-style lanes: "" is the main chat, anything else is a side thread.
  activePrivateThread: string;
  privateThreads: PrivateThreadSummary[];
  setActivePrivateThread: (threadId: string) => void;
  setPrivateThreads: (threads: PrivateThreadSummary[]) => void;
  botRuns: Record<string, BotRun>;
  closedBotRuns: Record<string, BotRunStatus>;
  receiveBotRun: (event: WSBotRunEvent) => void;
  receiveBotStream: (event: WSStreamEvent) => void;
  setRunCancelPending: (runId: string, pending: boolean) => void;
  // Lookup maps for O(1) reaction and read receipt updates (avoid O(n) array copies)
  reactionsByMessageId: Record<string, Record<string, string[]>>;
  readByMessageId: Record<string, string[]>;
  // Per-conversation last message preview cache (avoids O(n) reverse scan)
  lastPreviews: Record<string, { content: string; timestamp: number; sender: string }>;

  // Online users
  onlineUsers: string[];

  // User status list (all known users with online/offline status)
  userStatusList: UserStatus[];

  // Profile card
  selectedProfileUser: string | null;

  // Typing users
  typingUsers: string[];
  typingPreviews: Record<string, string>;

  // Chat context
  currentChat: CurrentChat;
  replyTo: ChatMessage | null;

  // Unread count
  unreadCount: number;
  unreadByConversation: Record<string, number>;
  /** Conversation the user is currently looking at, e.g. `public` or `dm-TokenBot`. */
  activeConversationKey: string;

  // Last read timestamps (per conversation) for "New messages" unread divider
  lastReadTimestamps: Record<string, number>;

  // Mention notifications
  latestMention: MentionNotification | null;

  // Blocked users
  blockedUsers: string[];

  // Pinned messages
  pinnedMessages: ChatMessage[];

  // Pinned conversations
  pinnedConversations: string[];

  // Muted conversations (legacy)
  mutedConversations: string[];

  // Notification preferences (per-conversation mute duration + preview toggle)
  notificationPrefs: Record<string, NotificationPref>;

  /** Whether reasoning traces are rendered at all (local preference). */
  showReasoning: boolean;

  // Archived conversations
  archivedConversations: string[];

  // Lightbox
  lightboxImage: string | null;

  // User profiles
  userProfiles: Record<string, UserProfile>;

  customEmojis: CustomEmoji[];
  translations: Record<string, string>; // messageId -> translated text

  // Polls
  polls: Record<string, PollData>;

  // Actions
  setView: (view: ViewState) => void;
  setUsername: (username: string) => void;
  setConnected: (connected: boolean) => void;
  setGuest: (isGuest: boolean) => void;
  // Auth modal & guest preview.
  showAuthModal: boolean;
  setShowAuthModal: (show: boolean) => void;
  addMessage: (message: ChatMessage) => void;
  deleteMessage: (id: string) => void;
  /** Marks an optimistic row rejected by the server; the bubble offers retry. */
  markMessageFailed: (clientMessageId: string, reason?: string) => void;
  /** Removes a row locally without contacting the server (retry / discard). */
  discardMessage: (id: string) => void;
  addSystemMessage: (content: string, timestamp: number, dedupeKey?: string) => void;
  setHistory: (messages: ChatMessage[]) => void;
  setPrivateBotHistory: (messages: ChatMessage[]) => void;
  addPrivateBotMessage: (message: ChatMessage) => void;
  prependHistory: (messages: ChatMessage[]) => void;
  setOnlineUsers: (users: string[]) => void;
  setUserStatusList: (users: UserStatus[]) => void;
  setSelectedProfileUser: (username: string | null) => void;
  setTypingUsers: (users: string[]) => void;
  addTypingUser: (username: string, preview?: string) => void;
  removeTypingUser: (username: string) => void;
  setCurrentChat: (chat: LegacyChatInput) => void;
  setReplyTo: (message: ChatMessage | null) => void;
  setUnreadCount: (count: number) => void;
  setActiveConversationKey: (key: string) => void;
  incrementConversationUnread: (key: string) => void;
  clearConversationUnread: (key: string) => void;
  clearAllConversationUnreads: () => void;
  markConversationRead: (key: string) => void;
  updateMessageReactions: (messageId: string, reactions: Record<string, string[]>) => void;
  editMessageInPlace: (messageId: string, content: string) => void;
  markMessagesReadBy: (reader: string) => void;
  setLatestMention: (mention: MentionNotification | null) => void;
  setBlockedUsers: (users: string[]) => void;
  addBlockedUser: (username: string) => void;
  removeBlockedUser: (username: string) => void;
  setPinnedMessages: (messages: ChatMessage[]) => void;
  setPinnedConversations: (keys: string[]) => void;
  addPinnedConversation: (key: string) => void;
  removePinnedConversation: (key: string) => void;
  setMutedConversations: (keys: string[]) => void;
  addMutedConversation: (key: string) => void;
  removeMutedConversation: (key: string) => void;
  setNotificationPrefs: (prefs: Record<string, NotificationPref>) => void;
  setShowReasoning: (show: boolean) => void;
  updateNotificationPref: (key: string, pref: NotificationPref) => void;
  setArchivedConversations: (keys: string[]) => void;
  addArchivedConversation: (key: string) => void;
  removeArchivedConversation: (key: string) => void;
  setLightboxImage: (url: string | null) => void;
  setUserProfile: (profile: UserProfile) => void;
  removeUserProfile: (username: string) => void;
  updateUserProfileStatus: (username: string, status: string) => void;
  setCustomEmojis: (emojis: CustomEmoji[]) => void;
  addCustomEmoji: (emoji: CustomEmoji) => void;
  removeCustomEmoji: (name: string) => void;
  setTranslation: (messageId: string, text: string) => void;
  updatePoll: (pollId: string, poll: PollData) => void;
  removePoll: (pollId: string) => void;
  reset: () => void;
}

export const useChatStore = create<ChatState>((set, get) => ({
  view: "chat" as ViewState,
  username: "",
  connected: false,
  isGuest: false,
  showAuthModal: false,
  messages: [],
  messageWindowRevision: 0,
  historyLoaded: false,
  // Private 1:1 with the assistant (TokenBot) — kept separate from the public
  // room list so the public chat and private assistant never mix.
  privateBotMessages: [],
  activePrivateThread: "",
  privateThreads: [],
  botRuns: {},
  closedBotRuns: {},
  reactionsByMessageId: {},
  readByMessageId: {},
  lastPreviews: {},
  onlineUsers: [],
  userStatusList: [],
  selectedProfileUser: null,
  typingUsers: [],
  typingPreviews: {},
  currentChat: { type: "public" },
  replyTo: null,
  unreadCount: 0,
  unreadByConversation: {},
  activeConversationKey: "public",
  lastReadTimestamps: loadLastReadTimestamps(""),
  latestMention: null,
  blockedUsers: [],
  pinnedMessages: [],
  pinnedConversations: [],
  mutedConversations: [],
  notificationPrefs: {},
  showReasoning: localStorage.getItem("tokendance:show-reasoning") !== "0",
  archivedConversations: [],
  lightboxImage: null,
  userProfiles: {},
  customEmojis: [],
  translations: {},
  polls: {},

  setView: (view) => set({ view }),
  setUsername: (username) => set((state) => ({
    username, lastReadTimestamps: loadLastReadTimestamps(username),
    ...(username !== state.username ? { ...clearActiveRuns(state), privateBotMessages: [], botRuns: {}, closedBotRuns: {} } : {}),
  })),
  setConnected: (connected) => set((state) => ({ connected, ...(!connected ? clearActiveRuns(state) : {}) })),
  // Rejected/late events return an empty patch; preserve the snapshot so a
  // burst of ignored events cannot keep scheduling React store updates.
  receiveBotRun: (event) => set((state) => {
    const patch = receiveRun(state, event);
    return Object.keys(patch).length ? patch : state;
  }),
  receiveBotStream: (event) => set((state) => {
    const patch = receiveStream(state, event);
    return Object.keys(patch).length ? patch : state;
  }),
  setRunCancelPending: (runId, pending) => set((state) => {
    const run = state.botRuns[runId];
    if (!run || !isActiveRun(run.status) || run.requested_by !== state.username) return state;
    return { botRuns: { ...state.botRuns, [runId]: { ...run, cancelPending: pending } } };
  }),
  setGuest: (isGuest) => set({ isGuest }),
  setShowAuthModal: (show) => set({ showAuthModal: show }),
  addMessage: (message) =>
    set((state) => {
      // Public and private delivery never share a transcript.
      if (message.private || !acceptFinal(state, message) || state.blockedUsers.includes(message.username)) return state;
      message = publicMessage({ ...message, id: message.message_id || message.id, streaming: false });
      const finished = finishMessage(state, message);
      const result = mergeMessageWindow(finished.messages ?? state.messages, [message], "append", MESSAGE_CAP);

      // Current frontend contract has one public room. Keep a public preview
      // only; legacy DM/group message fields remain backend compatibility data.
      if (!message.deleted && message.username !== "system" && message.content) {
        let content = message.content;
        if (content.length > 50) {
          content = content.slice(0, 47) + "...";
        }
        if (message.username === state.username) {
          content = "You: " + content;
        }
        state.lastPreviews.public = { content, timestamp: message.timestamp, sender: message.username };
      }

      return { ...finished, messages: result.messages, messageWindowRevision: result.revision };
    }),
  deleteMessage: (id) =>
    set((state) => ({
      messages: state.messages.filter((m) => m.id !== id),
    })),
  markMessageFailed: (clientMessageId, reason) =>
    set((state) => {
      const mark = (list: ChatMessage[]) => list.map((m) =>
        m.id === clientMessageId || m.client_message_id === clientMessageId
          ? { ...m, failed: true, failureReason: reason }
          : m,
      );
      return {
        messages: mark(state.messages),
        privateBotMessages: mark(state.privateBotMessages),
      };
    }),
  discardMessage: (id) =>
    set((state) => ({
      messages: state.messages.filter((m) => m.id !== id),
      privateBotMessages: state.privateBotMessages.filter((m) => m.id !== id),
    })),
  addSystemMessage: (content, timestamp, dedupeKey) =>
    set((state) => {
      // Coalescing rules for system lines (reconnect loops, repeated joins):
      // - Same dedupeKey as the last line → update that line in place.
      // - Identical consecutive system text → just bump its timestamp.
      const last = state.messages[state.messages.length - 1];
      if (last && last.username === "system") {
        if (dedupeKey && last.dedupeKey === dedupeKey) {
          const msgs = state.messages.slice(0, -1);
          msgs.push({ ...last, content, timestamp });
          return { messages: msgs, messageWindowRevision: state.messageWindowRevision + 1 };
        }
        if (last.content === content) {
          const msgs = state.messages.slice(0, -1);
          msgs.push({ ...last, timestamp });
          return { messages: msgs, messageWindowRevision: state.messageWindowRevision + 1 };
        }
      }
      const systemMsg: ChatMessage = {
        id: `sys-${timestamp}-${Math.random().toString(36).slice(2, 8)}`,
        username: "system",
        content,
        timestamp,
        dedupeKey,
      };
      const result = mergeMessageWindow(state.messages, [systemMsg], "append", MESSAGE_CAP);
      return { messages: result.messages, messageWindowRevision: result.revision };
    }),
  setHistory: (incoming) =>
    set((state) => {
      incoming = incoming.filter((message) => !message.private && acceptFinal(state, message)).map(publicMessage);
      const result = mergeMessageWindow(state.messages, incoming, "append", MESSAGE_CAP);
      // Populate only the public preview from history messages.
      for (const m of incoming) {
        if (m.deleted || m.username === "system" || !m.content) continue;
        let content = m.content;
        if (content.length > 50) {
          content = content.slice(0, 47) + "...";
        }
        if (m.username === state.username) {
          content = "You: " + content;
        }
        state.lastPreviews.public = { content, timestamp: m.timestamp, sender: m.username };
      }
      return {
        messages: sortChronologically(result.messages),
        messageWindowRevision: result.revision,
        historyLoaded: true,
      };
    }),
  setPrivateBotHistory: (incoming) =>
    set((state) => {
      incoming = incoming.filter((message) => acceptFinal(state, { ...message, private: true })).map(privateMessage);
      const result = mergeMessageWindow(state.privateBotMessages, incoming, "append", MESSAGE_CAP);
      return { privateBotMessages: sortChronologically(result.messages) };
    }),
  addPrivateBotMessage: (message) =>
    set((state) => {
      // mergeMessageWindow replaces an optimistic twin (matched by
      // client_message_id) with the persisted echo, avoiding duplicates.
      message = privateMessage({ ...message, private: true, id: message.message_id || message.id, streaming: false });
      if (!acceptFinal(state, message)) return state;
      const finished = finishMessage(state, message);
      const result = mergeMessageWindow(finished.privateBotMessages ?? state.privateBotMessages, [message], "append", MESSAGE_CAP);
      return { ...finished, privateBotMessages: result.messages };
    }),
  prependHistory: (incoming) =>
    set((state) => {
      incoming = incoming.filter((message) => !message.private && acceptFinal(state, message)).map(publicMessage);
      const result = mergeMessageWindow(state.messages, incoming, "prepend", HISTORY_CAP);
      // Fire event when no more history can be loaded (all duplicates or window full).
      if (result.addedCount === 0) {
        window.dispatchEvent(new CustomEvent(CHAT_EVENTS.noMoreHistory));
      }
      return { messages: sortChronologically(result.messages), messageWindowRevision: result.revision };
    }),
  setOnlineUsers: (onlineUsers) => set({ onlineUsers }),
  setUserStatusList: (userStatusList) => set({ userStatusList }),
  setSelectedProfileUser: (selectedProfileUser) => set({ selectedProfileUser }),
  setTypingUsers: (typingUsers) => set({ typingUsers }),
  addTypingUser: (username) =>
    set((state) => state.typingUsers.includes(username)
      ? state
      : { typingUsers: [...state.typingUsers, username] }),
  removeTypingUser: (username) =>
    set((state) => state.typingUsers.includes(username)
      ? { typingUsers: state.typingUsers.filter((u) => u !== username) }
      : state),
  setCurrentChat: () =>
    set((state) => {
      // Legacy callers may still pass dm/group while old state is being
      // cleaned. The current frontend contract always returns to public.
      const nextTimestamps = { ...state.lastReadTimestamps, public: Date.now() };
      try {
        localStorage.setItem(getLSLastReadKey(state.username), JSON.stringify(nextTimestamps));
      } catch { /* quota exceeded */ }
      return { currentChat: { type: "public" }, lastReadTimestamps: nextTimestamps };
    }),
  setReplyTo: (replyTo) => set({ replyTo }),
  setUnreadCount: (unreadCount) => set({ unreadCount }),
  setActiveConversationKey: (activeConversationKey) => set({ activeConversationKey }),
  setActivePrivateThread: (threadId) => set({ activePrivateThread: threadId }),
  setPrivateThreads: (threads) => set({ privateThreads: threads }),
  incrementConversationUnread: (key) =>
    set((state) => ({
      unreadByConversation: {
        ...state.unreadByConversation,
        [key]: (state.unreadByConversation[key] || 0) + 1,
      },
    })),
  clearConversationUnread: (key) =>
    set((state) => {
      if (!state.unreadByConversation[key]) return state;
      const next = { ...state.unreadByConversation };
      delete next[key];
      const nextTimestamps = { ...state.lastReadTimestamps, [key]: Date.now() };
      try {
        localStorage.setItem(getLSLastReadKey(state.username), JSON.stringify(nextTimestamps));
      } catch { /* quota exceeded */ }
      return { unreadByConversation: next, lastReadTimestamps: nextTimestamps };
    }),
  clearAllConversationUnreads: () => set({ unreadByConversation: {} }),
  markConversationRead: (key) =>
    set((state) => {
      const nextTimestamps = { ...state.lastReadTimestamps, [key]: Date.now() };
      try {
        localStorage.setItem(getLSLastReadKey(state.username), JSON.stringify(nextTimestamps));
      } catch { /* quota exceeded */ }
      const nextUnread = { ...state.unreadByConversation };
      delete nextUnread[key];
      return { lastReadTimestamps: nextTimestamps, unreadByConversation: nextUnread };
    }),
  updateMessageReactions: (messageId, reactions) =>
    set((state) => ({
      reactionsByMessageId: { ...state.reactionsByMessageId, [messageId]: reactions },
    })),
  editMessageInPlace: (messageId, content) =>
    set((state) => ({
      messages: state.messages.map((m) =>
        m.id === messageId ? { ...m, content, edited: true } : m,
      ),
    })),
  markMessagesReadBy: (reader) =>
    set((state) => {
      const next = { ...state.readByMessageId };
      let changed = false;
      for (const m of state.messages) {
        if (m.username !== state.username) continue;
        const existing = next[m.id] || m.read_by || [];
        if (existing.includes(reader)) continue;
        next[m.id] = [...existing, reader];
        changed = true;
      }
      if (!changed) return state;
      return { readByMessageId: next };
    }),
  setLatestMention: (latestMention) => set({ latestMention }),
  setBlockedUsers: (blockedUsers) => set({ blockedUsers }),
  addBlockedUser: (username) =>
    set((state) => ({
      blockedUsers: state.blockedUsers.includes(username)
        ? state.blockedUsers
        : [...state.blockedUsers, username],
    })),
  removeBlockedUser: (username) =>
    set((state) => ({
      blockedUsers: state.blockedUsers.filter((u) => u !== username),
    })),
  setPinnedMessages: (pinnedMessages) => set({ pinnedMessages }),
  setPinnedConversations: (pinnedConversations) => set({ pinnedConversations }),
  addPinnedConversation: (key) =>
    set((state) => ({
      pinnedConversations: state.pinnedConversations.includes(key)
        ? state.pinnedConversations
        : [...state.pinnedConversations, key],
    })),
  removePinnedConversation: (key) =>
    set((state) => ({
      pinnedConversations: state.pinnedConversations.filter((k) => k !== key),
    })),
  setMutedConversations: (mutedConversations) => set({ mutedConversations }),
  addMutedConversation: (key) =>
    set((state) => ({
      mutedConversations: state.mutedConversations.includes(key)
        ? state.mutedConversations
        : [...state.mutedConversations, key],
    })),
  removeMutedConversation: (key) =>
    set((state) => ({
      mutedConversations: state.mutedConversations.filter((k) => k !== key),
    })),
  setNotificationPrefs: (prefs) => set({ notificationPrefs: prefs }),
  setShowReasoning: (showReasoning) => {
    localStorage.setItem("tokendance:show-reasoning", showReasoning ? "1" : "0");
    set({ showReasoning });
  },
  updateNotificationPref: (key, pref) =>
    set((state) => ({
      notificationPrefs: { ...state.notificationPrefs, [key]: pref },
    })),
  setArchivedConversations: (archivedConversations) => set({ archivedConversations }),
  addArchivedConversation: (key) =>
    set((state) => ({
      archivedConversations: state.archivedConversations.includes(key)
        ? state.archivedConversations
        : [...state.archivedConversations, key],
    })),
  removeArchivedConversation: (key) =>
    set((state) => ({
      archivedConversations: state.archivedConversations.filter((k) => k !== key),
    })),
  setLightboxImage: (lightboxImage) => set({ lightboxImage }),
  setUserProfile: (profile) =>
    set((state) => ({
      userProfiles: { ...state.userProfiles, [profile.username]: profile },
    })),
  removeUserProfile: (username) =>
    set((state) => {
      const next = { ...state.userProfiles };
      delete next[username];
      return { userProfiles: next };
    }),
  updateUserProfileStatus: (username, status) =>
    set((state) => {
      const existing = state.userProfiles[username];
      if (!existing) return state;
      return {
        userProfiles: {
          ...state.userProfiles,
          [username]: { ...existing, status },
        },
      };
    }),
  setCustomEmojis: (customEmojis) => set({ customEmojis }),
  addCustomEmoji: (emoji) =>
    set((state) => ({ customEmojis: [...state.customEmojis, emoji] })),
  removeCustomEmoji: (name) =>
    set((state) => ({
      customEmojis: state.customEmojis.filter((e) => e.name !== name),
    })),
  updatePoll: (pollId, poll) =>
    set((state) => ({
      polls: { ...state.polls, [pollId]: poll },
    })),
  removePoll: (pollId) =>
    set((state) => {
      const next = { ...state.polls };
      delete next[pollId];
      return { polls: next };
    }),
  setTranslation: (messageId, text) =>
    set((state) => ({ translations: { ...state.translations, [messageId]: text } })),
  reset: () => {
    const state = get();
    if (state.username) {
      try {
        localStorage.removeItem(getLSLastReadKey(state.username));
      } catch { /* ignore */ }
    }
    set({
      view: "chat" as ViewState,
      username: "",
      connected: false,
      isGuest: false,
      showAuthModal: false,
      messages: [],
      messageWindowRevision: 0,
      historyLoaded: false,
      privateBotMessages: [],
      botRuns: {},
      closedBotRuns: {},
      reactionsByMessageId: {},
      readByMessageId: {},
      lastPreviews: {},
      onlineUsers: [],
      userStatusList: [],
      selectedProfileUser: null,
      typingUsers: [],
      typingPreviews: {},
      currentChat: { type: "public" },
      replyTo: null,
      unreadCount: 0,
      unreadByConversation: {},
      activeConversationKey: "public",
      lastReadTimestamps: {},
      latestMention: null,
      blockedUsers: [],
      pinnedMessages: [],
      pinnedConversations: [],
      mutedConversations: [],
      notificationPrefs: {},
      archivedConversations: [],
      lightboxImage: null,
      userProfiles: {},
      customEmojis: [],
      translations: {},
      polls: {},
    });
  },
}));
