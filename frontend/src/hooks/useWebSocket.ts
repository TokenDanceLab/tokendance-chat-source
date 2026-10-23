import { useEffect, useCallback, useRef } from "react";
import { useChatStore } from "@/stores/chatStore";
import { CHAT_EVENTS } from "@/lib/events";
import { APP_TITLE } from "@/lib/constants";
import { STORAGE_KEYS } from "@/lib/storage";
import { detectLanguage } from "@/i18n/translations";
import {
  chatAPI,
  type WSMessage,
  type ChatMessage,
  type WSChatMessage,
  type WSHistoryMessage,
  type WSPrivateHistoryMessage,
  type WSPrivateThreadsMessage,
  type WSUserEvent,
  type WSTypingEvent,
  type WSUserStatus,
  type WSReactionUpdate,
  type WSMessageEditBroadcast,
} from "@/lib/api";

// ─── Page title utilities ───

const BASE_TITLE = APP_TITLE;
let unreadTitleCount = 0;
let isTabActive = typeof document !== "undefined" ? !document.hidden : true;
let sharedWebSocketSubscriptionCount = 0;
let sharedWebSocketSubscriptionCleanup: (() => void) | null = null;

function updatePageTitle(): void {
  document.title = getPageTitle(unreadTitleCount, isTabActive);
}

function releaseSharedWebSocketSubscription() {
  sharedWebSocketSubscriptionCount = Math.max(0, sharedWebSocketSubscriptionCount - 1);
  if (sharedWebSocketSubscriptionCount === 0) {
    sharedWebSocketSubscriptionCleanup?.();
    sharedWebSocketSubscriptionCleanup = null;
  }
}

/** Pure computation — exported for testing. */
export function getPageTitle(unreadCount: number, tabActive: boolean): string {
  if (!tabActive && unreadCount > 0) {
    return `(${unreadCount}) ${BASE_TITLE}`;
  }
  return BASE_TITLE;
}

export function i18nSys(key: string, params?: Record<string, string>): string {
  if (params) {
    return JSON.stringify({ key, params });
  }
  return JSON.stringify({ key });
}

// Desktop notification helper
export function notifyMessage(title: string, body: string) {
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
  try {
    const n = new Notification(title, { body, icon: "/favicon.svg", silent: true });
    n.onclick = () => { window.focus(); n.close(); };
  } catch { /* ignore */ }
}

/** The privacy promise behind the per-conversation "message preview" switch. */
export function notificationBody(key: string, content: string): string {
  const pref = useChatStore.getState().notificationPrefs[key];
  if (pref?.showPreview !== false) return content;
  return detectLanguage() === "en-US" ? "New message" : "收到一条新消息";
}

/** Private assistant replies never expose their content in OS notifications. */
export function privateNotificationBody(assistantName: string): string {
  return detectLanguage() === "en-US"
    ? `New reply from ${assistantName}`
    : `${assistantName} 有新回复`;
}

/** Check if a conversation is muted, considering both legacy mutedConversations
 *  and per-conversation notification preferences with time-based muting. */
export function isConversationMuted(key: string): boolean {
  const state = useChatStore.getState();
  if (state.mutedConversations.includes(key)) return true;
  const pref = state.notificationPrefs[key];
  if (pref && pref.mutedUntil > Date.now()) return true;
  return false;
}

export function useWebSocket() {
  const typingTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(
    new Map(),
  );
  const prevStatusRef = useRef<Record<string, boolean>>({});
  const {
    setConnected,
    addMessage,
    setHistory,
    addPrivateBotMessage,
    setOnlineUsers,
    setUserStatusList,
    addSystemMessage,
    addTypingUser,
    removeTypingUser,
    updateMessageReactions,
    editMessageInPlace,
    setUnreadCount,
    deleteMessage,
    markMessagesReadBy,
    setLatestMention,
    setBlockedUsers,
    setPinnedMessages,
    setPinnedConversations,
    setMutedConversations,
    setNotificationPrefs,
    setArchivedConversations,
    setCustomEmojis,
    addCustomEmoji,
    removeCustomEmoji,
    setTranslation,
    updatePoll,
    removePoll,
  } = useChatStore();

  const connect = useCallback(
    async (name: string) => {
      try {
        await chatAPI.connect(name);
        setConnected(true);
        // Only clear isGuest if it wasn't explicitly set by the caller (guest join).
        if (!useChatStore.getState().isGuest) {
          useChatStore.getState().setGuest(false);
        }
        chatAPI.sendMarkRead();
        chatAPI.sendBlockList();
      } catch (err) {
        setConnected(false);
        throw err;
      }
    },
    [setConnected],
  );

  const disconnect = useCallback(() => {
    chatAPI.disconnect();
    setConnected(false);
  }, [setConnected]);

  const sendMessage = useCallback(
    (content: string, to?: string) => {
      const state = useChatStore.getState();
      // Optimistic: add message to store immediately so it appears without
      // waiting for the server echo. The real message replaces it via dedup.
      const tempId = `optimistic_${Date.now()}_${Math.random().toString(36).slice(2, 6)}`;
      const optimistic: import("@/lib/api").ChatMessage = {
        id: tempId,
        client_message_id: tempId,
        username: state.username,
        content,
        timestamp: Date.now(),
        edited: false,
      };
      if (to) {
        // Private bot thread — optimistic into the separate private list,
        // pinned to the lane the user is looking at.
        const lane = state.activePrivateThread;
        useChatStore.getState().addPrivateBotMessage({ ...optimistic, thread_id: lane });
        chatAPI.sendMessage(content, state.replyTo || undefined, tempId, to, lane);
      } else {
        addMessage(optimistic);
        window.dispatchEvent(new CustomEvent(CHAT_EVENTS.optimisticMessage, { detail: { id: tempId } }));
        chatAPI.sendMessage(content, state.replyTo || undefined, tempId);
      }
      // Clear reply after sending.
      useChatStore.getState().setReplyTo(null);
    },
    [addMessage],
  );

  const markRead = useCallback(() => {
    chatAPI.sendMarkRead();
    useChatStore.getState().clearConversationUnread("public");
    setUnreadCount(0);
    unreadTitleCount = 0;
    updatePageTitle();
  }, [setUnreadCount]);

  const sendReaction = useCallback((messageId: string, emoji: string) => {
    chatAPI.sendReaction(messageId, emoji);
  }, []);

  const sendMessageEdit = useCallback((messageId: string, content: string) => {
    chatAPI.sendMessageEdit(messageId, content);
  }, []);

  useEffect(() => {
    sharedWebSocketSubscriptionCount += 1;
    if (sharedWebSocketSubscriptionCount > 1 && sharedWebSocketSubscriptionCleanup) {
      return releaseSharedWebSocketSubscription;
    }

    // Tab visibility tracking.
    const handleVisibility = () => {
      isTabActive = !document.hidden;
      if (isTabActive) {
        unreadTitleCount = 0;
        updatePageTitle();
      }
    };
    document.addEventListener("visibilitychange", handleVisibility);

    // Notification permission is requested only from an explicit user action
    // (Settings → 桌面通知), never on connect — Chrome silently rejects
    // gesture-less requests and a surprise prompt on load is hostile UX.

    const unsubs: (() => void)[] = [];

    // Public chat message
    unsubs.push(
      chatAPI.on("message", (msg: WSMessage) => {
        const { id, client_message_id, username, content, parts, timestamp, reply_to_id, reply_to_content, reply_to_user, private: isPrivate, run_id, stream_id, message_id, to, room_id } =
          msg as WSChatMessage;
        // Private message (personal bot thread) → separate list, never the room.
        if (isPrivate) {
          const state = useChatStore.getState();
          const lane = (msg as WSChatMessage).thread_id ?? "";
          const laneKey = lane ? `dm-${username}:${lane}` : `dm-${username}`;
          if (lane === state.activePrivateThread) {
            state.addPrivateBotMessage({
              id, run_id, stream_id, message_id, to, room_id, private: true,
              // Live rows must carry their lane too: the transcript filters by
              // thread_id, so omitting it drops a side chat's reply into main.
              thread_id: lane,
              client_message_id,
              username,
              content,
              parts,
              timestamp: timestamp || Date.now(),
              reply_to_id,
              reply_to_content,
              reply_to_user,
            } as ChatMessage);
            removeTypingUser(username);
          } else {
            // Another lane moved: refresh the sidebar list so its preview and
            // ordering stay honest without paying a refetch per message. The
            // peer is the counterparty of this 1:1, whichever side sent it.
            const peer = username === state.username ? (to ?? "") : username;
            if (peer) chatAPI.sendListPrivateThreads(peer);
          }
          // Assistant replies own the unread badge for that lane; the echo of
          // the user's own message never does.
          if (username !== state.username) {
            if (!isTabActive || state.activeConversationKey !== laneKey) {
              state.incrementConversationUnread(laneKey);
            }
            if (!isTabActive && !isConversationMuted(laneKey)) {
              unreadTitleCount += 1;
              updatePageTitle();
              import("@/lib/sound").then((m) => m.playMessageSound());
              notifyMessage(username, privateNotificationBody(username));
            }
          }
          return;
        }
        addMessage({
          id, run_id, stream_id, message_id, room_id,
          client_message_id,
          username,
          content,
          parts,
          timestamp: timestamp || Date.now(),
          reply_to_id,
          reply_to_content,
          reply_to_user,
        } as ChatMessage);
        removeTypingUser(username);
        const viewingPublic = isTabActive && useChatStore.getState().activeConversationKey === "public";
        if (!viewingPublic && !isConversationMuted("public")) {
          useChatStore.getState().incrementConversationUnread("public");
          if (!isTabActive) {
            import("@/lib/sound").then((m) => m.playMessageSound());
            notifyMessage(username, notificationBody("public", content));
          }
        }
      }),
    );

    // History
    unsubs.push(
      chatAPI.on("history", (msg: WSMessage) => {
        const { messages, pagination } = msg as WSHistoryMessage;
        // `pagination` marks an explicit load_history page (older than the
        // current window). The join snapshot — first load and every reconnect
        // — is authoritative for the current window and must merge
        // chronologically instead of being prepended above newer messages.
        if (pagination) {
          useChatStore.getState().prependHistory(messages || []);
          // If fewer than 50 messages returned, we've hit the beginning.
          if (!messages || messages.length < 50) {
            window.dispatchEvent(new CustomEvent(CHAT_EVENTS.noMoreHistory));
          }
        } else {
          setHistory(messages || []);
        }
      }),
    );

    // Private 1:1 history answers `load_private`; it carries the caller's own
    // thread with the assistant (works for guests, who have no HTTP session).
    unsubs.push(
      chatAPI.on("private_history", (msg: WSMessage) => {
        const { messages, thread_id } = msg as WSPrivateHistoryMessage;
        // Ignore a stale answer for a lane the user has already left.
        if ((thread_id ?? "") !== useChatStore.getState().activePrivateThread) return;
        useChatStore.getState().setPrivateBotHistory(messages || [], thread_id ?? "");
      }),
    );

    // Sidebar lane list (Muse-style main chat + side threads).
    unsubs.push(
      chatAPI.on("private_threads", (msg: WSMessage) => {
        const { threads } = msg as WSPrivateThreadsMessage;
        useChatStore.getState().setPrivateThreads(threads || []);
      }),
    );

    // User joined
    unsubs.push(
      chatAPI.on("user_joined", (msg: WSMessage) => {
        const { username, online, timestamp } = msg as WSUserEvent;
        if (online) {
          setOnlineUsers(online);
        }
        // Skip self-join announcements: they fire on every (re)connect and are
        // pure noise for the local user.
        if (username && username !== useChatStore.getState().username) {
          addSystemMessage(
            i18nSys("system.userJoined", { username }),
            timestamp || Date.now(),
          );
        }
      }),
    );

    // User left
    unsubs.push(
      chatAPI.on("user_left", (msg: WSMessage) => {
        const { username, online, timestamp } = msg as WSUserEvent;
        if (online) {
          setOnlineUsers(online);
        }
        if (username && username !== useChatStore.getState().username) {
          addSystemMessage(
            i18nSys("system.userLeft", { username }),
            timestamp || Date.now(),
          );
        }
      }),
    );

    // Online users sync
    unsubs.push(
      chatAPI.on("online_users", (msg: WSMessage) => {
        const { online } = msg as { type: string; online: string[] };
        if (online) {
          setOnlineUsers(online);
        }
      }),
    );

    // Kicked — another session logged in with the same username.
    unsubs.push(
      chatAPI.on("kicked", (msg: WSMessage) => {
        const { content } = msg as { type: string; content: string };
        addSystemMessage(
          content || i18nSys("system.kicked"),
          Date.now(),
        );
        // Reset store state and disconnect.
        useChatStore.getState().reset();
        disconnect();
      }),
    );

    unsubs.push(
      chatAPI.on("account_logout", () => {
        localStorage.removeItem(STORAGE_KEYS.auth);
        localStorage.removeItem(STORAGE_KEYS.username);
        useChatStore.getState().reset();
        useChatStore.getState().setShowAuthModal(true);
        disconnect();
      }),
    );

    // User status / presence updates
    unsubs.push(
      chatAPI.on("user_status", (msg: WSMessage) => {
        const { users } = msg as WSUserStatus;
        if (users && users.length > 0) {
          const isFirstEvent = Object.keys(prevStatusRef.current).length === 0;
          if (!isFirstEvent) {
            for (const user of users) {
              const prevOnline = prevStatusRef.current[user.username];
              if (prevOnline === false && user.online === true) {
                addSystemMessage(
                  i18nSys("system.userOnline", { username: user.username }),
                  Date.now(),
                );
                import("@/lib/sound").then((m) => m.playOnlineSound());
              }
            }
          }
          const newMap: Record<string, boolean> = {};
          for (const user of users) {
            newMap[user.username] = user.online;
          }
          prevStatusRef.current = newMap;

          setUserStatusList(users);

          // Also populate userProfiles from user_status data.
          const { setUserProfile } = useChatStore.getState();
          for (const user of users) {
            if (user.display_name || user.avatar_url || user.status) {
              setUserProfile({
                username: user.username,
                display_name: user.display_name ?? "",
                avatar_url: user.avatar_url ?? "",
                bio: "",
                status: user.status ?? "",
                last_seen: user.last_seen,
                created_at: 0,
              });
            }
          }
        }
      }),
    );

    // Profile updated broadcast
    unsubs.push(
      chatAPI.on("profile_updated", (msg: WSMessage) => {
        const { username, display_name, avatar_url, bio, status } =
          msg as unknown as { username: string; display_name?: string; avatar_url?: string; bio?: string; status?: string };
        if (username) {
          useChatStore.getState().setUserProfile({
            username,
            display_name: display_name ?? "",
            avatar_url: avatar_url ?? "",
            bio: bio ?? "",
            status: status ?? "",
            last_seen: Date.now(),
            created_at: 0,
          });
        }
      }),
    );

    // Profile get response
    unsubs.push(
      chatAPI.on("profile_get", (msg: WSMessage) => {
        const { username, display_name, avatar_url, bio, status, last_seen } =
          msg as unknown as { username: string; display_name?: string; avatar_url?: string; bio?: string; status?: string; last_seen?: number };
        if (username) {
          useChatStore.getState().setUserProfile({
            username,
            display_name: display_name ?? "",
            avatar_url: avatar_url ?? "",
            bio: bio ?? "",
            status: status ?? "",
            last_seen: last_seen ?? 0,
            created_at: 0,
          });
        }
      }),
    );

    // Status updated broadcast
    unsubs.push(
      chatAPI.on("status_updated", (msg: WSMessage) => {
        const { username, status } = msg as unknown as { username: string; status: string };
        if (username) {
          useChatStore.getState().updateUserProfileStatus(username, status ?? "");
        }
      }),
    );

    // Quiet retries: the first two attempts stay silent (header status only);
    // only a sustained outage earns one transcript notice.
    let reconnectNoticeShown = false;
    unsubs.push(
      chatAPI.on("reconnecting", (msg: WSMessage) => {
        const { attempt } = msg as { type: string; attempt: number };
        setConnected(false);
        if (attempt < 2) return;
        reconnectNoticeShown = true;
        addSystemMessage(
          i18nSys("system.reconnecting", { attempt: String(attempt + 1) }),
          Date.now(),
          "reconnect",
        );
      }),
    );

    unsubs.push(
      chatAPI.on("reconnected", () => {
        useChatStore.getState().setConnected(true);
        if (!reconnectNoticeShown) return;
        reconnectNoticeShown = false;
        addSystemMessage(i18nSys("system.reconnected"), Date.now(), "reconnect");
      }),
    );

    unsubs.push(
      chatAPI.on("reconnect_failed", () => {
        addSystemMessage(i18nSys("system.reconnectFailed"), Date.now());
      }),
    );

    // Block list
    unsubs.push(
      chatAPI.on("block_list", (msg: WSMessage) => {
        const { blocked } = msg as { type: string; blocked: string[] };
        if (blocked) {
          setBlockedUsers(blocked);
        }
      }),
    );

    // Block confirmation
    unsubs.push(
      chatAPI.on("block", (msg: WSMessage) => {
        const { username: blockedUser } = msg as { type: string; username: string };
        if (blockedUser) {
          useChatStore.getState().addBlockedUser(blockedUser);
        }
      }),
    );

    // Unblock confirmation
    unsubs.push(
      chatAPI.on("unblock", (msg: WSMessage) => {
        const { username: unblockedUser } = msg as { type: string; username: string };
        if (unblockedUser) {
          useChatStore.getState().removeBlockedUser(unblockedUser);
        }
      }),
    );

    // Pinned messages list
    unsubs.push(
      chatAPI.on("pinned_list", (msg: WSMessage) => {
        const { messages } = msg as { type: string; messages: ChatMessage[] };
        if (messages) {
          setPinnedMessages(messages);
        }
      }),
    );

    // Pin event
    unsubs.push(
      chatAPI.on("pinned", (msg: WSMessage) => {
        const { id } = msg as { type: string; id: string; pinned_by: string; pinned_at: number };
        if (id) {
          const state = useChatStore.getState();
          const msgToPin = state.messages.find((m) => m.id === id);
          if (msgToPin) {
            setPinnedMessages([...state.pinnedMessages, msgToPin]);
          }
        }
      }),
    );

    // Unpin event
    unsubs.push(
      chatAPI.on("unpinned", (msg: WSMessage) => {
        const { id } = msg as { type: string; id: string };
        if (id) {
          const state = useChatStore.getState();
          setPinnedMessages(state.pinnedMessages.filter((m) => m.id !== id));
        }
      }),
    );

    // Pinned conversations list
    unsubs.push(
      chatAPI.on("pinned_conversations", (msg: WSMessage) => {
        const { keys } = msg as { type: string; keys: string[] };
        if (keys) {
          setPinnedConversations(keys);
        }
      }),
    );

    // Muted conversations list
    unsubs.push(
      chatAPI.on("muted_conversations", (msg: WSMessage) => {
        const { keys } = msg as { type: string; keys: string[] };
        if (keys) {
          setMutedConversations(keys);
        }
      }),
    );

    // Notification preferences — per-conversation mute duration + preview toggle.
    unsubs.push(
      chatAPI.on("notification_prefs", (msg: WSMessage) => {
        const data = msg as {
          type: string;
          key?: string;
          muted_until?: number;
          show_preview?: boolean;
          notif_prefs?: Array<{ key: string; muted_until: number; show_preview: boolean }>;
        };
        // Single conversation update (echo from notification_prefs_set)
        if (data.key) {
          useChatStore.getState().updateNotificationPref(data.key, {
            mutedUntil: data.muted_until ?? 0,
            showPreview: data.show_preview ?? true,
          });
          // Sync legacy muted conversations.
          const now = Date.now();
          if (data.muted_until && data.muted_until > now) {
            useChatStore.getState().addMutedConversation(data.key);
          } else {
            useChatStore.getState().removeMutedConversation(data.key);
          }
        }
        // Full list update (from notification_prefs_get or join handler)
        if (data.notif_prefs) {
          const prefs: Record<string, { mutedUntil: number; showPreview: boolean }> = {};
          for (const p of data.notif_prefs) {
            prefs[p.key] = { mutedUntil: p.muted_until, showPreview: p.show_preview };
          }
          setNotificationPrefs(prefs);
          // Sync legacy muted conversations from full list.
          const now = Date.now();
          const mutedKeys = data.notif_prefs
            .filter((p) => p.muted_until > now)
            .map((p) => p.key);
          setMutedConversations(mutedKeys);
        }
      }),
    );

    // Archived conversations list
    unsubs.push(
      chatAPI.on("archived_conversations", (msg: WSMessage) => {
        const { keys } = msg as { type: string; keys: string[] };
        if (keys) {
          setArchivedConversations(keys);
        }
      }),
    );

    // Message delete
    unsubs.push(
      chatAPI.on("message_delete", (msg: WSMessage) => {
        const { id } = msg as { type: string; id: string };
        if (id) {
          deleteMessage(id);
          removePoll(id);
        }
      }),
    );

    // Reaction update
    unsubs.push(
      chatAPI.on("reaction_update", (msg: WSMessage) => {
        const { id, reactions } = msg as WSReactionUpdate;
        if (id && reactions) {
          updateMessageReactions(id, reactions);
        }
      }),
    );

    // Message edit
    unsubs.push(
      chatAPI.on("message_edit", (msg: WSMessage) => {
        const { id, content, edited } = msg as WSMessageEditBroadcast;
        if (id && content && edited) {
          editMessageInPlace(id, content);
        }
      }),
    );

    unsubs.push(
      chatAPI.on("bot_run", (msg) => {
        useChatStore.getState().receiveBotRun(msg as import("@/lib/api").WSBotRunEvent);
      }),
      chatAPI.on("stream", (msg) => {
        const event = msg as import("@/lib/api").WSStreamEvent;
        useChatStore.getState().receiveBotStream(event);
        if (event.kind === "done" || event.kind === "error" || event.done) removeTypingUser(event.username);
      }),
      chatAPI.on("error", (msg) => {
        const event = msg as import("@/lib/api").WSErrorMessage;
        if (event.run_id) useChatStore.getState().setRunCancelPending(event.run_id, false);
        // A rejected send never reaches the server transcript. Mark the
        // optimistic row so the bubble can offer retry instead of pretending
        // the message was delivered.
        if (event.client_message_id) {
          useChatStore.getState().markMessageFailed(event.client_message_id, event.content);
        }
      }),
    );

    // Typing indicator — current frontend contract only renders public room typing.
    unsubs.push(
      chatAPI.on("typing", (msg: WSMessage) => {
        const { username: typingUser, context: typingCtx } = msg as WSTypingEvent;
        if (typingCtx && typingCtx !== "public") {
          return;
        }
        addTypingUser(typingUser);

        const existing = typingTimers.current.get(typingUser);
        if (existing) clearTimeout(existing);

        const timer = setTimeout(() => {
          removeTypingUser(typingUser);
          typingTimers.current.delete(typingUser);
        }, 10000);
        typingTimers.current.set(typingUser, timer);
      }),
    );

    // Typing stop — immediately remove user from typing list.
    unsubs.push(
      chatAPI.on("typing_stop", (msg: WSMessage) => {
        const { username: typingUser } = msg as WSTypingEvent;
        removeTypingUser(typingUser);
        const existing = typingTimers.current.get(typingUser);
        if (existing) clearTimeout(existing);
        typingTimers.current.delete(typingUser);
      }),
    );

    // Read receipt — someone read our messages.
    unsubs.push(
      chatAPI.on("read_receipt", (msg: WSMessage) => {
        const { from } = msg as { type: string; from: string };
        if (from) {
          markMessagesReadBy(from);
        }
      }),
    );

    // Mention notification — someone @mentioned us.
    unsubs.push(
      chatAPI.on("mention_notify", (msg: WSMessage) => {
        const { from, content, message_id, room_id, group: grp } = msg as {
          type: string;
          from: string;
          content: string;
          message_id: string;
          room_id?: string;
          group?: string;
        };
        if (from) {
          setLatestMention({
            from,
            content: (content || "").slice(0, 100),
            messageId: message_id || "",
            roomId: room_id,
            group: grp,
            timestamp: Date.now(),
          });
          import("@/lib/sound").then((m) => m.playMentionSound());
          // Flash title if tab not active.
          if (!isTabActive) {
            unreadTitleCount++;
            updatePageTitle();
          }
        }
      }),

      // @all / @everyone mention
      chatAPI.on("mention_all", (msg: WSMessage) => {
        const { from, content, message_id, room_id, group: grp } = msg as {
          type: string;
          from: string;
          content: string;
          message_id: string;
          room_id?: string;
          group?: string;
        };
        if (from) {
          setLatestMention({
            from: `${from} (@all)`,
            content: (content || "").slice(0, 100),
            messageId: message_id || "",
            roomId: room_id,
            group: grp,
            timestamp: Date.now(),
          });
          import("@/lib/sound").then((m) => m.playMentionSound());
          if (!isTabActive) {
            unreadTitleCount++;
            updatePageTitle();
          }
        }
      }),
    );

    // Translation result
    unsubs.push(
      chatAPI.on("translate_result", (msg: WSMessage) => {
        const { message_id, content } = msg as {
          type: string; message_id: string; content: string;
        };
        if (message_id && content) {
          setTranslation(message_id, content);
        }
      }),
    );

    // ─── Poll events ───

    // Poll created
    unsubs.push(
      chatAPI.on("poll_created", (msg: WSMessage) => {
        const { id, username, room_id, poll } = msg as {
          type: string; id: string; username: string; room_id?: string; poll: import("@/lib/api").PollData;
        };
        if (id && poll) {
          addMessage({
            id,
            username: username || poll.creator,
            content: poll.question,
            timestamp: poll.created_at || Date.now(),
            room_id,
          });
          updatePoll(id, poll);
        }
      }),
    );

    // Poll vote update
    unsubs.push(
      chatAPI.on("poll_vote_update", (msg: WSMessage) => {
        const { id, poll } = msg as {
          type: string; id: string; poll: import("@/lib/api").PollData; room_id?: string;
        };
        if (id && poll) {
          updatePoll(id, poll);
        }
      }),
    );

    // Poll closed
    unsubs.push(
      chatAPI.on("poll_closed", (msg: WSMessage) => {
        const { id, poll } = msg as {
          type: string; id: string; poll: import("@/lib/api").PollData; room_id?: string;
        };
        if (id && poll) {
          updatePoll(id, poll);
        }
      }),
    );

    sharedWebSocketSubscriptionCleanup = () => {
      document.removeEventListener("visibilitychange", handleVisibility);
      unsubs.forEach((unsub) => unsub());
      typingTimers.current.forEach((timer) => clearTimeout(timer));
      typingTimers.current.clear();
    };
    return releaseSharedWebSocketSubscription;
  }, [addMessage, setHistory, setOnlineUsers, addSystemMessage, addTypingUser, removeTypingUser, updateMessageReactions, editMessageInPlace, setUnreadCount, deleteMessage, markMessagesReadBy, setLatestMention, setBlockedUsers, setPinnedMessages, setPinnedConversations, setMutedConversations, setArchivedConversations, setCustomEmojis, addCustomEmoji, removeCustomEmoji, setNotificationPrefs, setTranslation, updatePoll, disconnect, addPrivateBotMessage, removePoll, setConnected, setUserStatusList]);

  return { connect, disconnect, sendMessage, markRead, sendReaction, sendMessageEdit };
}
