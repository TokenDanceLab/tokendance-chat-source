/**
 * Window-level events used to coordinate composer, transcript, and sidebar
 * surfaces without coupling their component trees.
 */
export const CHAT_EVENTS = {
  optimisticMessage: "tdchat:optimistic-message",
  noMoreHistory: "tdchat:no-more-history",
  closeEmojiPicker: "tdchat:close-emoji-picker",
  setComposer: "tdchat:set-composer",
  transcriptScrolled: "tdchat:transcript-scrolled",
  focusConversationSearch: "tdchat:focus-conversation-search",
  exitSelectMode: "tdchat:exit-select-mode",
  scrollToMessage: "tdchat:scroll-to-message",
  openSearch: "tdchat:open-search",
  viewThread: "tdchat:view-thread",
} as const;
