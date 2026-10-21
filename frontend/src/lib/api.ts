export interface WSMessage {
  type: string;
  [key: string]: unknown;
}

export interface PollData {
  id: string;
  room_id: string;
  creator: string;
  question: string;
  options: string[];
  multiple_choice: boolean;
  is_anonymous: boolean;
  is_closed: boolean;
  votes: Record<number, number>;
  voters: Record<number, string[]>;
  created_at: number;
}

export interface ChatMessage {
  id: string;
  client_message_id?: string;
  run_id?: string;
  message_id?: string;
  stream_id?: string;
  private?: boolean;
  /** Client-only draft marker; stable IDs are shared with the final message. */
  streaming?: boolean;
  username: string;
  content: string;
  timestamp: number;
  room_id?: string;
  edited?: boolean;
  reactions?: Record<string, string[]>;
  reply_to_id?: string;
  reply_to_content?: string;
  reply_to_user?: string;
  deleted?: boolean;
  to?: string;
  from?: string;
  group?: string;
  read_by?: string[];
  subtype?: string;
  poll?: PollData;
  thread_id?: string;
  mention_all?: boolean;
  /** Assistant reply parts; sources are registered by the server's web tools. */
  parts?: ChatPart[];
  /** Client-only: coalescing key for local system messages (never sent to server). */
  dedupeKey?: string;
  /** Client-only: the server rejected this optimistic message; nothing was delivered. */
  failed?: boolean;
  failureReason?: string;
}

/** One assistant-ui message part as transported over the chat WebSocket. */
export interface ChatPart {
  type: "text" | "reasoning" | "tool-call" | "source";
  text?: string;
  tool_call_id?: string;
  tool_name?: string;
  arguments?: string;
  result?: string;
  status?: "running" | "complete" | "error" | string;
  source_id?: string;
  title?: string;
  url?: string;
  cited?: boolean;
}

export interface ChatSourcePart extends ChatPart {
  type: "source";
  source_id: string;
  title: string;
  url: string;
  status: "discovered" | "fetched" | "failed";
  cited: boolean;
}

export interface WSChatMessage extends WSMessage {
  type: "message";
  id: string;
  client_message_id?: string;
  run_id?: string;
  message_id?: string;
  stream_id?: string;
  private?: boolean;
  to?: string;
  reply_to_id?: string;
  reply_to_content?: string;
  reply_to_user?: string;
  username: string;
  content: string;
  parts?: ChatPart[];
  timestamp: number;
  room_id?: string;
  edited?: boolean;
  reactions?: Record<string, string[]>;
}

export interface WSHistoryMessage extends WSMessage {
  type: "history";
  messages: ChatMessage[];
  room_id?: string;
  /** True for a `load_history` page; absent for the join/resync snapshot. */
  pagination?: boolean;
}

export interface WSUserEvent extends WSMessage {
  type: "user_joined" | "user_left";
  username: string;
  online: string[];
  timestamp: number;
}

export interface WSErrorMessage extends WSMessage {
  type: "error";
  content: string;
  code?: string;
  run_id?: string;
  /** Present when the error rejects a specific optimistic message. */
  client_message_id?: string;
}

export interface WSTypingEvent extends WSMessage {
  type: "typing";
  username: string;
  context?: string;
  to?: string;
  preview?: string;
}

export interface TypingContext {
  channel: "public";
  preview?: string;
}

export interface UserStatus {
  username: string;
  online: boolean;
  last_seen: number;
  display_name?: string;
  avatar_url?: string;
  status?: string;
}

export interface WSUserStatus extends WSMessage {
  type: "user_status";
  users: UserStatus[];
}

export interface WSStreamEvent extends WSMessage {
  type: "stream";
  username: string;
  content?: string;
  kind?: "text" | "reasoning" | "tool_call" | "tool_result" | "done" | "error";
  tool_call?: {
    id: string;
    name: string;
    arguments?: string;
    result?: string;
    status?: string;
  };
  parts?: ChatPart[];
  stream_id?: string;
  run_id?: string;
  message_id?: string;
  reply_to_id?: string;
  room_id?: string;
  to?: string;
  done?: boolean;
  private?: boolean;
}

export type BotRunStatus = "queued" | "running" | "completed" | "failed" | "cancelled";

export interface WSBotRunEvent extends WSMessage {
  type: "bot_run";
  run_id: string;
  message_id: string;
  stream_id: string;
  status: BotRunStatus;
  trigger_message_id: string;
  requested_by: string;
  room_id?: string;
  private?: boolean;
  to?: string;
  timestamp?: number;
}

export interface WSBotCancelRequest {
  type: "bot_cancel";
  run_id: string;
}

export interface WSJoinRequest {
  type: "join";
  username: string;
}

export interface WSSendMessage {
  type: "message";
  client_message_id?: string;
  content: string;
  reply_to_id?: string;
  reply_to_content?: string;
  reply_to_user?: string;
}

export interface WSMessageDelete {
  type: "message_delete";
  id: string;
}

export interface WSMarkRead {
  type: "mark_read";
}

// Link preview types
export interface LinkPreviewData {
  title: string;
  description: string;
  image: string;
  url: string;
  site_name?: string;
}

/** Public server configuration exposed by GET /api/config (no secrets). */
export interface ServerConfig {
  bot_name: string;
  model: string;
  llm_enabled: boolean;
  oidc_enabled: boolean;
  local_auth_enabled: boolean;
  /** Authoritative message limit; absent on older backends. */
  max_message_length?: number;
  capabilities?: { run_control?: boolean; web_search?: boolean; [key: string]: boolean | undefined };
}

// Search types
export interface SearchResult {
  id: string;
  username: string;
  content: string;
  timestamp: number;
  snippet: string;
  rank: number;
}

export interface CustomEmoji {
  id: string;
  name: string;
  url: string;
  uploader: string;
  room_id: string;
  created_at: number;
}

// Reaction types
export interface WSReactionRequest {
  type: "reaction";
  message_id: string;
  emoji: string;
}

export interface WSReactionUpdate extends WSMessage {
  type: "reaction_update";
  id: string;
  reactions: Record<string, string[]>;
}

// Message edit types
export interface WSMessageEditRequest {
  type: "message_edit";
  id: string;
  content: string;
}

export interface WSMessageEditBroadcast extends WSMessage {
  type: "message_edit";
  id: string;
  username: string;
  content: string;
  timestamp: number;
  edited: boolean;
}

// Profile types
export interface WSProfileUpdated extends WSMessage {
  type: "profile_updated" | "profile_get";
  username: string;
  display_name?: string;
  avatar_url?: string;
  bio?: string;
  status?: string;
  last_seen?: number;
}

export interface WSStatusUpdated extends WSMessage {
  type: "status_updated";
  username: string;
  status: string;
}

export type WSEventHandler = (msg: WSMessage) => void;

export const ErrorCode = {
  TIMEOUT: "ERR_TIMEOUT",
  CLOSED: "ERR_CLOSED",
  CANNOT_CONNECT: "ERR_CANNOT_CONNECT",
  AUTH_FAILED: "ERR_AUTH_FAILED",
} as const;
export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode];

export class ChatError extends Error {
  code: ErrorCode;
  constructor(code: ErrorCode, message: string) {
    super(message);
    this.code = code;
    this.name = "ChatError";
  }
}

class ChatAPI {
  private ws: WebSocket | null = null;
  private url: string;
  private handlers: Map<string, Set<WSEventHandler>> = new Map();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempt = 0;
  private maxReconnectAttempts = 8;
  private reconnectBaseDelay = 2000;
  private reconnectMaxDelay = 60000;
  private reconnectUsername: string | null = null;
  private wasReconnecting = false;
  private connectGeneration = 0;
  private outboundQueue: Array<Record<string, unknown>> = [];
  private pendingJoin:
    | {
        resolve: () => void;
        reject: (err: Error) => void;
      }
    | null = null;

  constructor(url: string) {
    this.url = url;
  }

  connect(username: string, preserveOutboundQueue = false): Promise<void> {
    return new Promise((resolve, reject) => {
      this.ws?.close();
      // A fresh user-initiated connect starts a clean outbox. Automatic
      // reconnects must keep it: send() queues while the socket is down.
      if (!preserveOutboundQueue) this.outboundQueue = [];
      const gen = ++this.connectGeneration;
      this.reconnectUsername = username;
      this.pendingJoin = { resolve, reject };
      this.ws = new WebSocket(this.url);

      const timeout = setTimeout(() => {
        if (gen !== this.connectGeneration) return;
        if (this.pendingJoin) {
          this.pendingJoin.reject(
            new ChatError(ErrorCode.TIMEOUT, "Connection timed out"),
          );
          this.pendingJoin = null;
        }
        this.ws?.close();
      }, 15000);

      this.ws.onopen = () => {
        if (gen !== this.connectGeneration) return;
        clearTimeout(timeout);
        if (!this.pendingJoin) return; // Timed out, ignore.
        this.reconnectAttempt = 0;
        this.send({ type: "join", username });
      };

      this.ws.onmessage = (event) => {
        if (gen !== this.connectGeneration) return;
        try {
          const data = JSON.parse(event.data) as WSMessage;

          // Handle error during join phase.
          if (data.type === "error" && this.pendingJoin) {
            clearTimeout(timeout);
            const code = typeof data.code === "string" ? data.code : "";
            if (
              this.wasReconnecting &&
              (code === "OIDC_REQUIRED" || code === "AUTH_REQUIRED")
            ) {
              this.reconnectUsername = null;
              this.wasReconnecting = false;
              this.dispatch("account_logout", { type: "account_logout" });
            }
            this.pendingJoin.reject(new Error(data.content as string));
            this.pendingJoin = null;
            this.ws?.close();
            return;
          }

          // Handle kick (duplicate login) during join phase.
          if (data.type === "kicked" && this.pendingJoin) {
            clearTimeout(timeout);
            this.pendingJoin.reject(new Error("Kicked by new login"));
            this.pendingJoin = null;
            // Don't close — the server already closed it.
            return;
          }

          // Handle kick while already connected — prevent reconnect loop.
          if (data.type === "kicked" || data.type === "account_logout") {
            this.reconnectUsername = null;
            this.wasReconnecting = false;
          }

          // Confirm join when we receive history.
          if (data.type === "history" && this.pendingJoin) {
            clearTimeout(timeout);
            this.pendingJoin.resolve();
            this.pendingJoin = null;
            this.flushOutboundQueue();
            if (this.wasReconnecting) {
              this.wasReconnecting = false;
              this.dispatch("reconnected", { type: "reconnected" });
            }
          }

          this.dispatch(data.type, data);
          this.dispatch("*", data);
        } catch {
          console.warn("Failed to parse WebSocket message:", event.data);
        }
      };

      this.ws.onclose = () => {
        // Stale close from a previous socket — ignore.
        if (gen !== this.connectGeneration) return;
        if (this.pendingJoin) {
          clearTimeout(timeout);
          this.pendingJoin.reject(
            new ChatError(ErrorCode.CLOSED, "Connection closed"),
          );
          this.pendingJoin = null;
          // Initial connection failed — do NOT auto-reconnect.
          // The caller (tryAutoConnect or AuthModal) decides the next step.
          return;
        }
        // Established connection dropped — attempt automatic reconnection.
        this.attemptReconnect();
      };

      this.ws.onerror = () => {
        if (gen !== this.connectGeneration) return;
        if (this.pendingJoin) {
          clearTimeout(timeout);
          this.pendingJoin.reject(
            new ChatError(ErrorCode.CANNOT_CONNECT, "Cannot connect to server"),
          );
          this.pendingJoin = null;
        }
      };
    });
  }

  private attemptReconnect() {
    const username = this.reconnectUsername;
    if (!username) return;

    if (this.reconnectAttempt >= this.maxReconnectAttempts) {
      console.warn("Max reconnection attempts reached", { attempt: this.reconnectAttempt, username: this.reconnectUsername });
      this.dispatch("reconnect_failed", { type: "reconnect_failed", attempt: this.reconnectAttempt });
      return;
    }

    this.wasReconnecting = true;

    // Exponential backoff: 1s, 2s, 4s, 8s, 16s, ..., capped at 30s.
    const rawDelay = Math.min(
      this.reconnectBaseDelay * Math.pow(2, this.reconnectAttempt),
      this.reconnectMaxDelay,
    );
    // Jitter: +/- 20% to prevent thundering herd.
    const jitter = rawDelay * 0.2 * (Math.random() * 2 - 1);
    const delay = Math.round(rawDelay + jitter);

    const attempt = this.reconnectAttempt;
    this.dispatch("reconnecting", {
      type: "reconnecting",
      attempt,
      delay,
    });

    this.reconnectTimer = setTimeout(() => {
      this.reconnectAttempt++;
      this.connect(username, true).catch(() => {
        // Error already logged in connect; onclose will trigger next attempt.
      });
    }, delay);
  }

  get readyState(): number {
    return this.ws?.readyState ?? WebSocket.CLOSED;
  }

  send(data: Record<string, unknown>): void {
    if (this.ws?.readyState === WebSocket.OPEN && (!this.pendingJoin || data.type === "join")) {
      this.ws.send(JSON.stringify(data));
      return;
    }
    // Queue anything that isn't a join while the socket is connecting, open-but-
    // pending, or already closed — instead of dropping it. This is what stopped a
    // message showing as "sent" in the UI while the server never received it (a
    // guest typing during a brief reconnect). The queue flushes on the next onopen
    // (flushOutboundQueue) and is capped to bound staleness.
    if (data.type !== "join") {
      this.outboundQueue.push(data);
      if (this.outboundQueue.length > 100) {
        this.outboundQueue.shift();
      }
    }
  }

  private flushOutboundQueue(): void {
    if (this.ws?.readyState !== WebSocket.OPEN || this.pendingJoin) return;
    const queued = this.outboundQueue.splice(0);
    for (const item of queued) {
      this.ws.send(JSON.stringify(item));
    }
  }

  /** Cancellation is never queued across a disconnect. */
  cancelBotRun(runId: string): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN || this.pendingJoin) return false;
    const request: WSBotCancelRequest = { type: "bot_cancel", run_id: runId };
    this.ws.send(JSON.stringify(request));
    return true;
  }

  sendMessage(content: string, replyTo?: ChatMessage, clientMessageId?: string, to?: string): void {
    if (replyTo) {
      this.send({
        type: "message",
        client_message_id: clientMessageId,
        content,
        reply_to_id: replyTo.id,
        reply_to_content: replyTo.content,
        reply_to_user: replyTo.username,
        ...(to ? { to } : {}),
      });
    } else {
      this.send({
        type: "message",
        client_message_id: clientMessageId,
        content,
        ...(to ? { to } : {}),
      });
    }
  }

  sendThreadReply(threadId: string, content: string, to?: string): void {
    this.send({
      type: "message",
      content,
      thread_id: threadId,
      ...(to ? { to } : {}),
    });
  }

  requestThreadMessages(parentMessageId: string): void {
    this.send({
      type: "thread_messages",
      parent_message_id: parentMessageId,
    });
  }

  deleteMessage(id: string): void {
    this.send({ type: "message_delete", id });
  }

  sendMarkRead(): void {
    this.send({ type: "mark_read", context: "public" });
  }

  sendRoomLeave(): void {
    this.send({ type: "room_leave" });
  }

  sendTypingStart(ctx?: TypingContext): void {
    this.send({ type: "typing_start", context: ctx?.channel ?? "public", preview: ctx?.preview });
  }

  sendTypingStop(ctx?: TypingContext): void {
    this.send({ type: "typing_stop", context: ctx?.channel ?? "public" });
  }

  sendReaction(messageId: string, emoji: string): void {
    this.send({ type: "reaction", message_id: messageId, emoji });
  }

  sendMessageEdit(messageId: string, content: string): void {
    this.send({ type: "message_edit", id: messageId, content });
  }

  sendSetTopic(topic: string): void {
    this.send({ type: "set_topic", topic });
  }

  sendBlock(username: string): void {
    this.send({ type: "block", username });
  }

  sendUnblock(username: string): void {
    this.send({ type: "unblock", username });
  }

  sendBlockList(): void {
    this.send({ type: "block_list" });
  }

  sendLoadHistory(before: number): void {
    this.send({ type: "load_history", timestamp: before });
  }

  sendPinMessage(messageId: string): void {
    this.send({ type: "pin_message", id: messageId });
  }

  sendUnpinMessage(messageId: string): void {
    this.send({ type: "unpin_message", id: messageId });
  }

  sendSetNotificationPrefs(key: string, mutedUntil: number, showPreview: boolean): void {
    this.send({ type: "notification_prefs_set", key, muted_until: mutedUntil, show_preview: showPreview });
  }

  sendGetNotificationPrefs(): void {
    this.send({ type: "notification_prefs_get" });
  }

  sendPollCreate(question: string, options: string[], multipleChoice: boolean, isAnonymous: boolean): void {
    this.send({
      type: "poll_create",
      poll: {
        question,
        options,
        multiple_choice: multipleChoice,
        is_anonymous: isAnonymous,
      },
    });
  }

  sendPollVote(pollId: string, optionIndex: number): void {
    this.send({ type: "poll_vote", id: pollId, option_index: optionIndex });
  }

  sendPollClose(pollId: string): void {
    this.send({ type: "poll_close", id: pollId });
  }

  sendProfileUpdate(profile: {
    display_name?: string;
    avatar_url?: string;
    bio?: string;
    status?: string;
  }): void {
    this.send({ type: "profile_update", ...profile });
  }

  sendProfileGet(username?: string): void {
    this.send({ type: "profile_get", username });
  }

  sendStatusUpdate(status: string): void {
    this.send({ type: "status_update", status });
  }

  async fetchLinkPreview(url: string): Promise<LinkPreviewData | null> {
    try {
      const resp = await fetch(`/api/link-preview?url=${encodeURIComponent(url)}`);
      if (!resp.ok) return null;
      return await resp.json() as LinkPreviewData;
    } catch {
      return null;
    }
  }

  async fetchServerConfig(): Promise<ServerConfig | null> {
    try {
      const resp = await fetch("/api/config", { redirect: "manual" });
      if (!resp.ok) return null;
      return await resp.json() as ServerConfig;
    } catch {
      return null;
    }
  }

  async uploadImage(file: File): Promise<{ url?: string; error?: string }> {
    try {
      const formData = new FormData();
      formData.append("file", file);
      const resp = await fetch("/api/upload/image", {
        method: "POST",
        credentials: "same-origin",
        body: formData,
      });
      const data = (await resp.json().catch(() => ({}))) as { url?: string; error?: string };
      if (!resp.ok || !data.url) return { error: data.error || "upload failed" };
      return { url: data.url };
    } catch {
      return { error: "upload failed" };
    }
  }

  async uploadEmoji(file: File, name: string): Promise<string | null> {
    try {
      const formData = new FormData();
      formData.append("file", file);
      formData.append("name", name);
      const resp = await fetch("/api/emoji/upload", {
        method: "POST",
        credentials: "same-origin",
        body: formData,
      });
      if (!resp.ok) return null;
      const data = await resp.json() as { url: string };
      return data.url;
    } catch {
      return null;
    }
  }

  sendCustomEmojiAdd(name: string, url: string): void {
    this.send({ type: "custom_emoji_add", name, url });
  }

  sendCustomEmojiDelete(name: string): void {
    this.send({ type: "custom_emoji_delete", name });
  }

  sendCustomEmojiList(): void {
    this.send({ type: "custom_emoji_list" });
  }

  async exportChat(conversation: "public", format: 'json' | 'text', username?: string): Promise<Blob> {
    const params = new URLSearchParams({ conversation, format });
    if (username) params.set("username", username);
    const resp = await fetch(`/api/export?${params}`, {
      credentials: "same-origin",
    });
    if (!resp.ok) {
      throw new Error("Export failed");
    }
    return await resp.blob();
  }

  async searchMessages(query: string, roomID?: string): Promise<SearchResult[]> {
    try {
      const params = new URLSearchParams({ q: query });
      if (roomID) params.set("room", roomID);
      const resp = await fetch(`/api/search?${params}`, {
        credentials: "same-origin",
      });
      if (!resp.ok) return [];
      const data = await resp.json();
      return data.results ?? data as SearchResult[];
    } catch {
      return [];
    }
  }

  // fetchMessagesBetween loads the private 1:1 thread with a user (e.g. the
  // private TokenBot conversation). Requires an authenticated session.
  async fetchMessagesBetween(username: string): Promise<ChatMessage[]> {
    try {
      const params = new URLSearchParams({ to: username, limit: "100" });
      const resp = await fetch(`/api/messages?${params}`, {
        credentials: "same-origin",
        redirect: "manual",
      });
      if (!resp.ok) return [];
      const data = await resp.json();
      return data.messages ?? [];
    } catch {
      return [];
    }
  }

  on(event: string, handler: WSEventHandler): () => void {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, new Set());
    }
    this.handlers.get(event)!.add(handler);

    return () => {
      this.handlers.get(event)?.delete(handler);
    };
  }

  private dispatch(event: string, data: WSMessage): void {
    const handlers = this.handlers.get(event);
    if (handlers) {
      handlers.forEach((handler) => {
        try {
          handler(data);
        } catch (err) {
          console.error(`Error in handler for event "${event}":`, err);
        }
      });
    }
  }

  sendTranslateMessage(messageId: string, content: string, targetLang?: string): void {
    this.send({ type: "translate_message", message_id: messageId, content, to: targetLang || "" });
  }

  disconnect(): void {
    ++this.connectGeneration; // Stale onclose from old socket will be ignored
    this.wasReconnecting = false;
    this.reconnectUsername = null;
    this.pendingJoin = null;
    this.outboundQueue = [];
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    // Note: we intentionally do NOT clear handlers here.
    // Handlers are managed by component lifecycle (on/off).
  }
}

function getDefaultWSURL(): string {
  // Use current page host in production, localhost in dev.
  const host = window.location.host || "localhost:8080";
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${host}/ws`;
}

export const chatAPI = new ChatAPI(getDefaultWSURL());

// --- Auth API (HTTP-based, not WebSocket) ---

export interface SessionResponse {
  authenticated: boolean;
  username?: string;
  kind?: "local" | "oidc";
  csrf_token?: string;
}

let sessionCSRFToken = "";

export async function fetchSession(): Promise<SessionResponse> {
  const resp = await fetch("/api/session", {
    cache: "no-store",
    credentials: "same-origin",
  });
  if (!resp.ok) {
    sessionCSRFToken = "";
    return { authenticated: false };
  }
  const session = await resp.json() as SessionResponse;
  sessionCSRFToken = session.authenticated ? (session.csrf_token ?? "") : "";
  return session;
}

async function currentCSRFToken(): Promise<string> {
  if (sessionCSRFToken) return sessionCSRFToken;
  const session = await fetchSession();
  return session.authenticated ? (session.csrf_token ?? "") : "";
}

export async function logout(): Promise<void> {
  const csrfToken = await currentCSRFToken();
  const response = await fetch("/api/logout", {
    method: "POST",
    credentials: "same-origin",
    headers: csrfToken ? { "X-CSRF-Token": csrfToken } : undefined,
  });
  if (!response.ok) {
    throw new Error(`logout failed: ${response.status}`);
  }
  sessionCSRFToken = "";
}

export async function logoutFromAccount(): Promise<string> {
  const csrfToken = await currentCSRFToken();
  if (!csrfToken) {
    throw new Error("Account logout requires an authenticated session");
  }
  const response = await fetch("/api/logout/account", {
    method: "POST",
    credentials: "same-origin",
    headers: { "X-CSRF-Token": csrfToken },
  });
  if (!response.ok) {
    throw new Error(`Account logout failed: ${response.status}`);
  }
  const body = await response.json() as { redirect_url?: string };
  if (!body.redirect_url) {
    throw new Error("Account logout did not return a redirect URL");
  }
  sessionCSRFToken = "";
  return body.redirect_url;
}

export interface RegisterResponse {
  success: boolean;
  username: string;
}

export interface LoginResponse {
  success: boolean;
  username: string;
}

export interface InviteCode {
  code: string;
  creator: string;
  max_uses: number;
  use_count: number;
  created_at: number;
}

export async function registerUser(username: string, password: string, inviteCode: string, turnstileToken?: string | null): Promise<RegisterResponse> {
  const resp = await fetch("/api/register", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password, invite_code: inviteCode, cf_turnstile_response: turnstileToken ?? "" }),
  });
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({ error: "Registration failed" }));
    throw new Error((data as { error?: string }).error || "Registration failed");
  }
  return await resp.json() as RegisterResponse;
}

export async function loginUser(username: string, password: string, turnstileToken?: string | null): Promise<LoginResponse> {
  const resp = await fetch("/api/login", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password, cf_turnstile_response: turnstileToken ?? "" }),
  });
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({ error: "Login failed" }));
    const msg = (data as { error?: string }).error || "Login failed";
    if (resp.status === 401) {
      throw new ChatError(ErrorCode.AUTH_FAILED, msg);
    }
    throw new Error(msg);
  }
  return await resp.json() as LoginResponse;
}

export async function generateInviteCode(username: string, maxUses: number = 5): Promise<{ code: string }> {
  const resp = await fetch("/api/invite/generate", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, max_uses: maxUses }),
  });
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({ error: "Failed to generate invite code" }));
    throw new Error((data as { error?: string }).error || "Failed to generate invite code");
  }
  return await resp.json() as { code: string };
}

export async function listInviteCodes(username: string): Promise<InviteCode[]> {
  const resp = await fetch(`/api/invite/list?username=${encodeURIComponent(username)}`, {
    credentials: "same-origin",
  });
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({ error: "Failed to list invite codes" }));
    throw new Error((data as { error?: string }).error || "Failed to list invite codes");
  }
  const json = await resp.json() as { codes: InviteCode[] };
  return json.codes || [];
}

export async function fetchPublicMessages(limit = 100): Promise<ChatMessage[]> {
  try {
    const params = new URLSearchParams({ limit: String(limit) });
    const resp = await fetch(`/api/messages?${params}`, { redirect: "manual" });
    if (!resp.ok) return [];
    const data = await resp.json() as { messages?: ChatMessage[] };
    return data.messages || [];
  } catch {
    return [];
  }
}
