import { STORAGE_KEYS } from "@/lib/storage";

export type Language = "zh-CN" | "en-US";

export interface TranslationDict {
  join: {
    title: string;
    subtitle: string;
    placeholder: string;
    buttonJoin: string;
    buttonConnecting: string;
    footer: string;
    welcomeHint: string;
    errorEmpty: string;
    errorTooShort: string;
    errorTooLong: string;
    errorInvalidChars: string;
    buttonLogin: string;
    buttonRegister: string;
    buttonGuest: string;
    orText: string;
  };
  auth: {
    register: string;
    login: string;
    username: string;
    password: string;
    confirmPassword: string;
    inviteCode: string;
    registerButton: string;
    loginButton: string;
    loginOrRegister: string;
    guestUpgradeHint: string;
    haveAccount: string;
    noAccount: string;
    guestLogin: string;
    invalidCode: string;
    codeUsed: string;
    fillAllFields: string;
    passwordMinLength: string;
    passwordMaxLength: string;
    confirmNotMatch: string;
    loginFailed: string;
    signingIn: string;
    back: string;
    backToLogin: string;
    oidcLoginButton: string;
    or: string;
    oidcError: string;
    accountLogout: string;
    logoutFailed: string;
    accountLogoutFailed: string;
    accountLoggedOut: string;
    sessionBoundary: string;
  };
  invite: {
    inviteCodes: string;
    generateCode: string;
    maxUses: string;
    copyCode: string;
    noCodes: string;
    usesLeft: string;
  };
  chat: {
    roomName: string;
    subtitle: string;
    onlineCount: string;
    invite: string;
    inviteText: string;
    inviteCopied: string;
    inviteFailed: string;
    dmLabel: string;
    leave: string;
    disconnect: string;
    publicChat: string;
    deletedMessage: string;
    guestWarning: string;
    webSearchAvailable: string;
  };
  sidebar: {
    conversations: string;
    publicChat: string;
    publicChatSub: string;
    you: string;
    assistants: string;
    online: string;
    aiAssistants: string;
    closeSidebar: string;
    connecting: string;
    guestMode: string;
    unread: string;
    accountMenu: string;
    sourceAndLicense: string;
  };
  info: {
    title: string;
    publicTitle: string;
    agent: string;
    model: string;
    capabilities: string;
    webSearch: string;
    timeTools: string;
    memory: string;
    running: string;
    replyingYou: string;
    replyingTo: string;
    recentSources: string;
    noSources: string;
    members: string;
    noMembers: string;
    messages: string;
    sources: string;
    runs: string;
    thread: string;
    threadHint: string;
    activity: string;
    noActivity: string;
    today: string;
    yesterday: string;
    room: string;
    ambient: string;
    ambientHint: string;
  };
  transcript: {
    loading: string;
    emptyTitle: string;
    emptyDescription: string;
    emptyDmTitle: string;
    emptyDmDescription: string;
    newMessages: string;
    loadingOlder: string;
    loadErrorRetry: string;
    newMessagesDivider: string;
    olderMessages: string;
    selected: string;
    contextSelect: string;
    contextCopy: string;
    contextDelete: string;
    selectAll: string;
    copySelected: string;
    suggestCapabilities: string;
    suggestSummary: string;
    suggestCode: string;
    typing: string;
    welcomeAssistant: string;
    welcomeAssistantHint: string;
  };
  input: {
    placeholder: string;
    assistantPlaceholder: string;
    replyTo: string;
    characters: string;
    voiceStart: string;
    voiceStop: string;
    voiceFailed: string;
    escapeToCancel: string;
    cancel: string;
    save: string;
    editingMessage: string;
    send: string;
    pasteFileUnsupported: string;
    mentionSuggestions: string;
    sendHint: string;
    mentionHint: string;
    image: string;
    uploadingImage: string;
    imageUploadFailed: string;
    imageTooLarge: string;
    attach: string;
  };
  message: {
    edited: string;
    copy: string;
    delete: string;
    read: string;
    readBy: string;
    sent: string;
    copied: string;
    edit: string;
    pin: string;
    react: string;
    translate: string;
    contextMenu: string;
    seenBy: string;
    addReaction: string;
    block: string;
    blockConfirm: string;
    blocked: string;
    translated: string;
    generating: string;
    reasoning: string;
    toolRunning: string;
    toolComplete: string;
    toolFailed: string;
    toolArguments: string;
    toolResult: string;
    toolCurrentTime: string;
    toolSearchPublic: string;
    toolSearchPrivate: string;
    sources: string;
    sourceFetched: string;
    sourceDiscovered: string;
    sourceFailed: string;
    sourceCited: string;
    sourceOpen: string;
    sourcesShowAll: string;
    sourcesShowLess: string;
    sendFailed: string;
    retry: string;
    stopped: string;
  };
  system: {
    userJoined: string;
    userLeft: string;
    userOnline: string;
    connectionLost: string;
    reconnecting: string;
    reconnected: string;
    disconnected: string;
    reconnectFailed: string;
    botReplyFailed: string;
    botReplyStopped: string;
    updateReady: string;
    refresh: string;
    kicked: string;
    typing: string;
    typingTwo: string;
    typingMany: string;
  };
  botRun: {
    label: string;
    queued: string;
    running: string;
    stopping: string;
    stop: string;
    stopLabel: string;
    yourReply: string;
    replyTo: string;
    unavailable: string;
    queueFull: string;
    budgetExceeded: string;
    completed: string;
    failed: string;
    cancelled: string;
  };
  error: {
    timeout: string;
    closed: string;
    cannotConnect: string;
    unknown: string;
    somethingWentWrong: string;
    reloadMessage: string;
    reload: string;
  };
  lang: {
    switchTo: string;
    label: string;
  };
  more: {
    label: string;
  };
  mention: {
    mentionedYou: string;
    view: string;
    dismiss: string;
  };
  profile: {
    justNow: string;
    minutesAgo: string;
    today: string;
    yesterday: string;
  };
  search: {
    placeholder: string;
    typeToSearch: string;
    notFound: string;
    notFoundInConversation: string;
    searchError: string;
    toggleSearch: string;
    inConversation: string;
    matchCount: string;
    noMatchesInConversation: string;
    pressCtrlF: string;
  };
  emoji: {
    search: string;
    recent: string;
    noResults: string;
    openPicker: string;
    smileys: string;
    gestures: string;
    hearts: string;
    objects: string;
    misc: string;
    custom: string;
    uploadEmoji: string;
    deleteEmoji: string;
    noCustomEmoji: string;
  };
  settings: {
    title: string;
    sound: string;
    soundOn: string;
    soundOff: string;
    notificationPrefs: string;
    privateNotificationHint: string;
    showPreview: string;
    muteFor1h: string;
    muteFor8h: string;
    muteFor24h: string;
    muteForever: string;
    unmute: string;
    mutedConversations: string;
    noMutedConversations: string;
    blockedUsers: string;
    unblock: string;
    desktopNotifications: string;
    notificationsEnable: string;
    notificationsGranted: string;
    notificationsDenied: string;
    notificationsBlocked: string;
    previewOn: string;
    previewOff: string;
    profile: string;
    appearance: string;
    notifications: string;
    data: string;
    account: string;
    themeLight: string;
    themeDark: string;
    themeSystem: string;
    testSound: string;
    exportData: string;
    myAccount: string;
    openSettings: string;
    assistantSection: string;
    showReasoning: string;
    showReasoningHint: string;
    webSearchStatus: string;
    ambientStatus: string;
    ambientHint: string;
    capabilityOn: string;
    capabilityOff: string;
    about: string;
    language: string;
    accountSection: string;
    accountGuest: string;
    accountMember: string;
  };
  slash: {
    me: string;
    topic: string;
    shrug: string;
    tableflip: string;
  };
  thread: {
    replies: string;
    replyCount: string;
    replyPlaceholder: string;
    close: string;
  };
  editor: {
    bold: string;
    italic: string;
    strikethrough: string;
    code: string;
    quote: string;
    link: string;
    preview: string;
    linkUrl: string;
    formatting: string;
  };
  export: {
    exportJson: string;
    exportText: string;
    exportSuccess: string;
    exportError: string;
  };
  file: {
    uploading: string;
    downloadFile: string;
    fileSize: string;
    dropFilesHere: string;
  };
  poll: {
    closed: string;
    finalResults: string;
    vote: string;
    votes: string;
  };
  a11y: {
    close: string;
    back: string;
    clearSearch: string;
    closeSidebar: string;
    openSidebar: string;
    collapseSidebar: string;
    expandSidebar: string;
    toggleInfo: string;
    closeInfo: string;
    moreActions: string;
    addAttachment: string;
    removeImage: string;
    ok: string;
    uploadFile: string;
    prevResult: string;
    nextResult: string;
    closeSearch: string;
    zoomOut: string;
    zoomIn: string;
    copyCode: string;
    audioSeek: string;
    exitSelect: string;
    prevMonth: string;
    nextMonth: string;
    hour: string;
    minute: string;
    scrollToBottom: string;
    online: string;
    showPassword: string;
    hidePassword: string;
  };
}

const zhCN: TranslationDict = {
  join: {
    title: "TokenDance Chat",
    subtitle: "公共聊天室 · AI 助手 @TokenBot 在线陪伴",
    placeholder: "你的用户名...",
    buttonJoin: "加入聊天",
    buttonConnecting: "连接中...",
    footer: "公共聊天室 · 文明交流",
    welcomeHint: "支持 Markdown、表情反应、公共聊天室与 TokenBot 私聊",
    errorEmpty: "请输入用户名",
    errorTooShort: "用户名至少需要2个字符",
    errorTooLong: "用户名不能超过20个字符",
    errorInvalidChars: "用户名只能包含中文、英文、数字和下划线",
    buttonLogin: "登录",
    buttonRegister: "注册",
    buttonGuest: "游客加入",
    orText: "或者",
  },
  auth: {
    register: "注册账号",
    login: "登录",
    username: "用户名",
    password: "密码",
    confirmPassword: "确认密码",
    inviteCode: "邀请码",
    registerButton: "注册",
    loginButton: "登录",
    loginOrRegister: "登录 / 注册",
    guestUpgradeHint: "游客身份，登录后保留昵称与消息",
    haveAccount: "已有账号？去登录",
    noAccount: "还没有账号？去注册",
    guestLogin: "返回游客模式",
    invalidCode: "邀请码无效或已过期",
    codeUsed: "邀请码已达最大使用次数",
    fillAllFields: "请填写所有字段后点击注册",
    passwordMinLength: "密码至少需要8个字符",
    passwordMaxLength: "密码不能超过72个字符",
    confirmNotMatch: "两次输入的密码不一致",
    loginFailed: "用户名或密码错误",
    signingIn: "正在登录...",
    back: "返回",
    backToLogin: "返回登录",
    oidcLoginButton: "使用 TokenDance Account 继续",
    or: "或",
    oidcError: "账号登录失败",
    accountLogout: "退出 TokenDance Account",
    logoutFailed: "退出 Chat 失败，请重试",
    accountLogoutFailed: "暂时无法退出 TokenDance Account，请重试",
    accountLoggedOut: "已退出 TokenDance Account",
    sessionBoundary: "Chat 只保留自己的 HttpOnly 会话，不会把 Account 令牌交给浏览器。",
  },
  invite: {
    inviteCodes: "邀请码管理",
    generateCode: "生成邀请码",
    maxUses: "最大使用次数",
    copyCode: "复制",
    noCodes: "还没有生成邀请码",
    usesLeft: "{{used}}/{{max}}",
  },
  chat: {
    roomName: "公共聊天",
    subtitle: "在线聊天室",
    onlineCount: "{{count}} 人在线",
    invite: "邀请",
    inviteText: "来 TokenDance Chat 一起聊",
    inviteCopied: "邀请链接已复制",
    inviteFailed: "无法复制邀请链接",
    dmLabel: "私聊",
    leave: "离开聊天",
    disconnect: "断开连接",
    publicChat: "公共聊天",
    deletedMessage: "此消息已被删除",
    guestWarning: "游客模式：你的身份未经验证，其他人可以使用相同用户名",
    webSearchAvailable: "可搜索网页",
  },
  sidebar: {
    conversations: "会话",
    publicChat: "公共聊天",
    publicChatSub: "公共聊天室",
    you: "你",
    assistants: "助手",
    online: "在线",
    aiAssistants: "私人助手",
    closeSidebar: "关闭侧边栏",
    connecting: "连接中...",
    guestMode: "游客模式",
    unread: "条未读",
    accountMenu: "账户菜单",
    sourceAndLicense: "源码与许可证",
  },
  info: {
    title: "对话信息",
    publicTitle: "聊天信息",
    agent: "助手",
    model: "模型",
    capabilities: "能力",
    webSearch: "可搜索网页",
    timeTools: "查询时间",
    memory: "对话记忆",
    running: "正在回复",
    replyingYou: "正在回复你的请求",
    replyingTo: "正在回复 {{name}}",
    recentSources: "最近引用",
    noSources: "本段对话还没有引用来源",
    members: "在线成员",
    noMembers: "暂时只有你在线",
    messages: "消息",
    sources: "引用",
    runs: "运行",
    thread: "本段对话",
    threadHint: "统计本次私聊的消息数。引用的网页和运行记录会随时间清理。",
    activity: "最近活动",
    noActivity: "还没有可显示的活动",
    today: "今天",
    yesterday: "昨天",
    room: "房间",
    ambient: "主动参与",
    ambientHint: "无需 @，TokenBot 会自行判断是否接话。",
  },
  transcript: {
    loading: "加载消息中...",
    emptyTitle: "暂无消息",
    emptyDescription: "成为第一个发送消息的人吧！",
    emptyDmTitle: "开始私聊",
    emptyDmDescription: "这是你和 TokenBot 的私人对话，消息不会出现在公共聊天室。",
    newMessages: "{{count}} 条新消息",
    loadingOlder: "加载更早的消息...",
    loadErrorRetry: "加载失败，点击重试",
    newMessagesDivider: "新消息",
    olderMessages: "更早的消息",
    selected: "已选择 {{count}} 条",
    contextSelect: "选择",
    contextCopy: "复制",
    contextDelete: "删除",
    selectAll: "全选",
    copySelected: "复制选中",
    suggestCapabilities: "你能做什么？",
    suggestSummary: "帮我总结这里的讨论",
    suggestCode: "帮我写段代码",
    typing: "{{names}} 正在输入…",
    welcomeAssistant: "有什么可以帮你？",
    welcomeAssistantHint: "TokenBot 可以回答问题、总结讨论，也能帮你写代码。",
  },
  input: {
    placeholder: "输入消息...",
    assistantPlaceholder: "发消息给 {{name}}...",
    replyTo: "回复",
    characters: "{{current}}/{{max}}",
    voiceStart: "语音输入",
    voiceStop: "停止语音输入",
    voiceFailed: "语音输入不可用",
    escapeToCancel: "按 Esc 取消",
    cancel: "取消",
    save: "保存",
    editingMessage: "编辑消息",
    send: "发送",
    pasteFileUnsupported: "暂不支持发送图片或文件",
    mentionSuggestions: "提及建议",
    sendHint: "Enter 发送 · Shift+Enter 换行",
    mentionHint: "输入 @ 提及成员",
    image: "图片",
    uploadingImage: "正在上传图片…",
    imageUploadFailed: "图片上传失败，请重试",
    imageTooLarge: "图片不能超过 5MB",
    attach: "添加",
  },
  message: {
    edited: "（已编辑）",
    copy: "复制",
    delete: "删除",
    read: "已读",
    readBy: "已读用户",
    sent: "已发送",
    sendFailed: "发送失败",
    retry: "重试",
    stopped: "已停止生成",
    copied: "已复制",
    edit: "编辑",
    pin: "置顶",
    react: "添加表情",
    translate: "翻译",
    contextMenu: "消息操作",
    seenBy: "{{n}} 人已读",
    addReaction: "添加表情",
    block: "屏蔽 @{{name}}",
    blockConfirm: "再次点击确认屏蔽 @{{name}}",
    blocked: "已屏蔽 @{{name}}",
    translated: "已翻译",
    generating: "正在生成回复",
    reasoning: "思考过程",
    toolRunning: "调用中",
    toolComplete: "已完成",
    toolFailed: "失败",
    toolArguments: "参数",
    toolResult: "结果",
    toolCurrentTime: "查询时间",
    toolSearchPublic: "搜索公共消息",
    toolSearchPrivate: "搜索对话记录",
    sources: "网页来源",
    sourceFetched: "已读取",
    sourceDiscovered: "仅发现 · 未读取",
    sourceFailed: "读取失败",
    sourceCited: "已引用",
    sourceOpen: "在新标签页打开",
    sourcesShowAll: "显示其余 {{count}} 个来源",
    sourcesShowLess: "收起来源",
  },
  system: {
    userJoined: "{{username}} 加入了聊天室",
    userLeft: "{{username}} 离开了聊天室",
    userOnline: "{{username}} 上线了",
    connectionLost: "连接已断开，正在尝试重新连接...",
    reconnecting: "正在重新连接 (第 {{attempt}} 次)...",
    reconnected: "已重新连接",
    disconnected: "未连接 — 重新连接后重试",
    reconnectFailed: "连接已断开，请刷新页面。",
    botReplyFailed: "这次回复没有完成，请重试。",
    botReplyStopped: "回复已停止。",
    updateReady: "新版本已就绪",
    refresh: "刷新",
    kicked: "您的账号已在其他地方登录，当前连接已断开。",
    typing: "{{username}} 正在输入...",
    typingTwo: "{{name1}} 和 {{name2}} 正在输入...",
    typingMany: "{{name}} 和另外 {{count}} 人正在输入...",
  },
  botRun: {
    label: "TokenBot 回复状态",
    queued: "排队中",
    running: "正在回复",
    stopping: "正在停止…",
    stop: "停止",
    stopLabel: "停止回复：{{message}}",
    yourReply: "你的请求",
    replyTo: "回复 {{name}}",
    unavailable: "此回复已不可操作，请稍后重试。",
    queueFull: "回复队列已满，请稍后再试。",
    budgetExceeded: "当前回复额度已用完，请稍后再试。",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
  },
  error: {
    timeout: "连接超时，请检查服务器是否运行",
    closed: "连接已关闭",
    cannotConnect: "无法连接到聊天服务器",
    unknown: "连接服务器失败，请确保服务器正在运行",
    somethingWentWrong: "出错了",
    reloadMessage: "发生了意外错误，请尝试刷新页面。",
    reload: "刷新页面",
  },
  lang: {
    switchTo: "English",
    label: "切换语言",
  },
  more: {
    label: "更多",
  },
  mention: {
    mentionedYou: "提到了你",
    view: "查看",
    dismiss: "关闭",
  },
  profile: {
    justNow: "刚刚",
    minutesAgo: "{{n}}分钟前",
    today: "今天",
    yesterday: "昨天",
  },
  search: {
    placeholder: "搜索消息...",
    typeToSearch: "输入关键词搜索消息",
    notFound: "未找到消息",
    notFoundInConversation: "此对话中无消息",
    searchError: "搜索出错，请重试",
    toggleSearch: "切换搜索",
    inConversation: "搜索当前对话",
    matchCount: "{{n}} 条匹配",
    noMatchesInConversation: "当前对话无匹配",
    pressCtrlF: "Ctrl+F 搜索",
  },
  emoji: {
    search: "搜索表情...",
    recent: "最近使用",
    noResults: "未找到表情",
    openPicker: "插入表情",
    smileys: "表情",
    gestures: "手势",
    hearts: "爱心",
    objects: "物品",
    misc: "其他",
    custom: "自定义表情",
    uploadEmoji: "上传表情",
    deleteEmoji: "删除表情",
    noCustomEmoji: "暂无自定义表情",
  },
  settings: {
    title: "设置",
    sound: "音效",
    soundOn: "音效已开启",
    soundOff: "音效已关闭",
    notificationPrefs: "通知偏好",
    privateNotificationHint: "仅提示有新回复，不显示私聊正文",
    showPreview: "消息预览",
    muteFor1h: "静音 1 小时",
    muteFor8h: "静音 8 小时",
    muteFor24h: "静音 24 小时",
    muteForever: "永久静音",
    unmute: "取消静音",
    mutedConversations: "已静音会话",
    noMutedConversations: "暂无静音会话",
    blockedUsers: "已屏蔽用户",
    unblock: "解除屏蔽",
    desktopNotifications: "桌面通知",
    notificationsEnable: "开启通知",
    notificationsGranted: "通知已开启",
    notificationsDenied: "被浏览器阻止，请在站点设置中允许通知",
    notificationsBlocked: "已被阻止",
    previewOn: "预览已开启",
    previewOff: "预览已关闭",
    profile: "个人资料",
    appearance: "外观",
    notifications: "通知",
    data: "数据",
    account: "账号",
    themeLight: "浅色",
    themeDark: "深色",
    themeSystem: "跟随系统",
    testSound: "测试提示音",
    exportData: "导出聊天记录",
    myAccount: "我的账号",
    openSettings: "打开设置",
    assistantSection: "助手",
    showReasoning: "显示思考过程",
    showReasoningHint: "在回复上方折叠显示模型的思考摘要",
    webSearchStatus: "网页搜索",
    ambientStatus: "主动参与",
    ambientHint: "没有 @ 时，TokenBot 也会自行判断是否接话",
    capabilityOn: "已启用",
    capabilityOff: "未启用",
    about: "关于",
    language: "语言",
    accountSection: "账号",
    accountGuest: "游客模式",
    accountMember: "已登录",
  },
  slash: {
    me: "以动作方式发送消息",
    topic: "修改聊天室话题",
    shrug: "¯\\_(ツ)_/¯",
    tableflip: "(╯°□°)╯︵ ┻━┻",
  },
  thread: {
    replies: "条回复",
    replyCount: "{count} 条回复",
    replyPlaceholder: "回复此消息...",
    close: "关闭",
  },
  editor: {
    bold: "加粗",
    italic: "斜体",
    strikethrough: "删除线",
    code: "代码",
    quote: "引用",
    link: "链接",
    preview: "预览",
    linkUrl: "输入链接",
    formatting: "Markdown 格式",
  },
  export: {
    exportJson: "导出为 JSON",
    exportText: "导出为文本",
    exportSuccess: "聊天记录导出成功",
    exportError: "导出聊天记录失败",
  },
  file: {
    uploading: "上传中...",
    downloadFile: "下载文件",
    fileSize: "{{size}}",
    dropFilesHere: "拖放文件到此处",
  },
  poll: {
    closed: "投票已关闭",
    finalResults: "最终结果",
    vote: "投票",
    votes: "{{count}} 票",
  },
  a11y: {
    close: "关闭",
    back: "返回",
    clearSearch: "清除搜索",
    closeSidebar: "关闭侧边栏",
    openSidebar: "打开侧边栏",
    collapseSidebar: "收起侧边栏",
    expandSidebar: "展开侧边栏",
    toggleInfo: "显示或隐藏对话信息",
    closeInfo: "关闭对话信息",
    moreActions: "更多操作",
    addAttachment: "添加附件",
    removeImage: "移除图片",
    ok: "确定",
    uploadFile: "上传文件",
    prevResult: "上一个结果",
    nextResult: "下一个结果",
    closeSearch: "关闭搜索",
    zoomOut: "缩小",
    zoomIn: "放大",
    copyCode: "复制代码",
    audioSeek: "音频进度条",
    exitSelect: "退出选择模式",
    prevMonth: "上个月",
    nextMonth: "下个月",
    hour: "小时",
    minute: "分钟",
    scrollToBottom: "回到底部",
    online: "在线",
    showPassword: "显示密码",
    hidePassword: "隐藏密码",
  },

};


const enUS: TranslationDict = {
  join: {
    title: "TokenDance Chat",
    subtitle: "Public chat · AI assistant @TokenBot at your service",
    placeholder: "Your username...",
    buttonJoin: "Join Chat",
    buttonConnecting: "Connecting...",
    footer: "Public Chat Room · Be respectful",
    welcomeHint: "Markdown, emoji reactions, the public room and a private TokenBot chat",
    errorEmpty: "Please enter a username",
    errorTooShort: "Username must be at least 2 characters",
    errorTooLong: "Username cannot exceed 20 characters",
    errorInvalidChars:
      "Username can only contain letters, numbers, Chinese characters, and underscores",
    buttonLogin: "Login",
    buttonRegister: "Register",
    buttonGuest: "Join as Guest",
    orText: "or",
  },
  auth: {
    register: "Register Account",
    login: "Login",
    username: "Username",
    password: "Password",
    confirmPassword: "Confirm Password",
    inviteCode: "Invite Code",
    registerButton: "Register",
    loginButton: "Login",
    loginOrRegister: "Log in / Sign up",
    guestUpgradeHint: "Guest mode — log in to keep your name and messages",
    haveAccount: "Already have an account? Log in",
    noAccount: "Don't have an account? Register",
    guestLogin: "Back to Guest Mode",
    invalidCode: "Invalid or expired invite code",
    codeUsed: "Invite code has reached maximum uses",
    fillAllFields: "Please fill in all fields to register",
    passwordMinLength: "Password must be at least 8 characters",
    passwordMaxLength: "Password must be at most 72 characters",
    confirmNotMatch: "Passwords do not match",
    loginFailed: "Invalid username or password",
    signingIn: "Signing in...",
    back: "Back",
    backToLogin: "Back to Login",
    oidcLoginButton: "Continue with TokenDance Account",
    or: "or",
    oidcError: "Account sign-in failed",
    accountLogout: "Sign out of TokenDance Account",
    logoutFailed: "Could not leave Chat. Please try again.",
    accountLogoutFailed: "TokenDance Account sign-out is unavailable. Please try again.",
    accountLoggedOut: "Signed out of TokenDance Account",
    sessionBoundary: "Chat keeps only its own HttpOnly session; Account tokens stay on the server.",
  },
  invite: {
    inviteCodes: "Invite Codes",
    generateCode: "Generate Code",
    maxUses: "Max Uses",
    copyCode: "Copy",
    noCodes: "No invite codes generated yet",
    usesLeft: "{{used}}/{{max}}",
  },
  chat: {
    roomName: "Public Chat",
    subtitle: "Online Chat Room",
    onlineCount: "{{count}} online",
    invite: "Invite",
    inviteText: "Join TokenDance Chat",
    inviteCopied: "Invite link copied",
    inviteFailed: "Could not copy the invite link",
    dmLabel: "Direct message",
    leave: "Leave chat",
    disconnect: "Disconnect",
    publicChat: "Public Chat",
    deletedMessage: "This message was deleted",
    guestWarning: "Guest mode: your identity is unverified. Others may use the same username.",
    webSearchAvailable: "Web search available",
  },
  sidebar: {
    conversations: "Conversations",
    publicChat: "Public Chat",
    publicChatSub: "Public Chat Room",
    you: "You",
    assistants: "Assistants",
    online: "Online",
    aiAssistants: "Private Assistant",
    closeSidebar: "Close sidebar",
    connecting: "Connecting...",
    guestMode: "Guest mode",
    unread: "unread",
    accountMenu: "Account menu",
    sourceAndLicense: "Source and license",
  },
  info: {
    title: "Conversation info",
    publicTitle: "Chat info",
    agent: "Assistant",
    model: "Model",
    capabilities: "Capabilities",
    webSearch: "Web search",
    timeTools: "Time lookup",
    memory: "Thread memory",
    running: "Replying",
    replyingYou: "Replying to your request",
    replyingTo: "Replying to {{name}}",
    recentSources: "Recent sources",
    noSources: "No cited sources in this conversation yet",
    members: "Online members",
    noMembers: "You're the only one online",
    messages: "Messages",
    sources: "Sources",
    runs: "Runs",
    thread: "This conversation",
    threadHint: "Counts messages in this private chat. Sources and runs age out over time.",
    activity: "Recent activity",
    noActivity: "No activity to show yet",
    today: "Today",
    yesterday: "Yesterday",
    room: "Room",
    ambient: "Proactive replies",
    ambientHint: "No @ needed — TokenBot decides when to join.",
  },
  transcript: {
    loading: "Loading messages...",
    emptyTitle: "No messages yet",
    emptyDescription: "Be the first to send a message!",
    emptyDmTitle: "Start your private chat",
    emptyDmDescription: "This is your private conversation with TokenBot. Messages never appear in the public room.",
    newMessages: "{{count}} new messages",
    loadingOlder: "Loading older messages...",
    loadErrorRetry: "Failed to load — tap to retry",
    newMessagesDivider: "New messages",
    olderMessages: "Older messages",
    selected: "{{count}} selected",
    contextSelect: "Select",
    contextCopy: "Copy",
    contextDelete: "Delete",
    selectAll: "Select All",
    copySelected: "Copy Selected",
    suggestCapabilities: "What can you do?",
    suggestSummary: "Catch me up on this thread",
    suggestCode: "Help me write some code",
    typing: "{{names}} is typing…",
    welcomeAssistant: "How can I help you today?",
    welcomeAssistantHint: "TokenBot can answer questions, summarize a discussion, and write code.",
  },
  input: {
    placeholder: "Type a message...",
    assistantPlaceholder: "Message {{name}}...",
    replyTo: "Reply to",
    characters: "{{current}}/{{max}}",
    voiceStart: "Voice input",
    voiceStop: "Stop voice input",
    voiceFailed: "Voice input unavailable",
    escapeToCancel: "Escape to cancel",
    cancel: "Cancel",
    save: "Save",
    editingMessage: "Edit message",
    send: "Send",
    pasteFileUnsupported: "Sending images or files is not supported",
    mentionSuggestions: "Mention suggestions",
    sendHint: "Enter to send · Shift+Enter for a new line",
    mentionHint: "Type @ to mention",
    image: "Image",
    uploadingImage: "Uploading image…",
    imageUploadFailed: "Image upload failed, please retry",
    imageTooLarge: "Image must be under 5MB",
    attach: "Add",
  },
  message: {
    edited: "(edited)",
    copy: "Copy",
    delete: "Delete",
    read: "Read",
    readBy: "Read by",
    sent: "Sent",
    sendFailed: "Not sent",
    retry: "Retry",
    stopped: "Generation stopped",
    copied: "Copied",
    edit: "Edit",
    pin: "Pin",
    react: "React",
    translate: "Translate",
    contextMenu: "Message actions",
    seenBy: "Seen by {{n}}",
    addReaction: "Add reaction",
    block: "Block @{{name}}",
    blockConfirm: "Click again to block @{{name}}",
    blocked: "Blocked @{{name}}",
    translated: "Translated",
    generating: "Generating reply",
    reasoning: "Reasoning",
    toolRunning: "Running",
    toolComplete: "Completed",
    toolFailed: "Failed",
    toolArguments: "Arguments",
    toolResult: "Result",
    toolCurrentTime: "Check time",
    toolSearchPublic: "Search public messages",
    toolSearchPrivate: "Search this chat",
    sources: "Web sources",
    sourceFetched: "Read",
    sourceDiscovered: "Discovered · Not read",
    sourceFailed: "Could not read",
    sourceCited: "Cited",
    sourceOpen: "Open in a new tab",
    sourcesShowAll: "Show {{count}} more sources",
    sourcesShowLess: "Show fewer sources",
  },
  system: {
    userJoined: "{{username}} joined the chat",
    userLeft: "{{username}} left the chat",
    userOnline: "{{username}} is now online",
    connectionLost: "Connection lost, attempting to reconnect...",
    reconnecting: "Reconnecting (attempt {{attempt}})...",
    reconnected: "Reconnected",
    disconnected: "Not connected — retrying...",
    reconnectFailed: "Connection lost. Please reload the page.",
    botReplyFailed: "This reply did not finish. Please try again.",
    botReplyStopped: "Reply stopped.",
    updateReady: "A new version is ready",
    refresh: "Refresh",
    kicked: "Your account was logged in elsewhere. This connection has been closed.",
    typing: "{{username}} is typing...",
    typingTwo: "{{name1}} and {{name2}} are typing...",
    typingMany: "{{name}} and {{count}} others are typing...",
  },
  botRun: {
    label: "TokenBot reply status",
    queued: "Queued",
    running: "Replying",
    stopping: "Stopping…",
    stop: "Stop",
    stopLabel: "Stop reply: {{message}}",
    yourReply: "Your request",
    replyTo: "Reply to {{name}}",
    unavailable: "This reply is no longer available. Please try again later.",
    queueFull: "The reply queue is full. Please try again shortly.",
    budgetExceeded: "The reply limit has been reached. Please try again later.",
    completed: "Completed",
    failed: "Failed",
    cancelled: "Cancelled",
  },
  error: {
    timeout: "Connection timed out. Please check if the server is running",
    closed: "Connection closed",
    cannotConnect: "Could not connect to the chat server",
    unknown: "Failed to connect to server. Please ensure the server is running",
    somethingWentWrong: "Something went wrong",
    reloadMessage: "An unexpected error occurred. Please try reloading the page.",
    reload: "Reload",
  },
  lang: {
    switchTo: "中文",
    label: "Switch language",
  },
  more: {
    label: "More",
  },
  mention: {
    mentionedYou: "mentioned you",
    view: "View",
    dismiss: "Dismiss",
  },
  profile: {
    justNow: "just now",
    minutesAgo: "{{n}}m ago",
    today: "Today",
    yesterday: "Yesterday",
  },
  search: {
    placeholder: "Search messages...",
    typeToSearch: "Type to search messages",
    notFound: "No messages found",
    notFoundInConversation: "No messages in this conversation",
    searchError: "Search error — please try again",
    toggleSearch: "toggle search",
    inConversation: "Search in conversation",
    matchCount: "{{n}} matches",
    noMatchesInConversation: "No matches in conversation",
    pressCtrlF: "Ctrl+F Search",
  },
  emoji: {
    search: "Search emoji...",
    recent: "Recent",
    noResults: "No emojis found",
    openPicker: "Insert emoji",
    smileys: "Smileys",
    gestures: "Gestures",
    hearts: "Hearts",
    objects: "Objects",
    misc: "Misc",
    custom: "Custom Emoji",
    uploadEmoji: "Upload Emoji",
    deleteEmoji: "Delete Emoji",
    noCustomEmoji: "No custom emojis",
  },
  settings: {
    title: "Settings",
    sound: "Sound",
    soundOn: "Sound on",
    soundOff: "Sound off",
    notificationPrefs: "Notification Prefs",
    privateNotificationHint: "Notify about new replies without showing private message text",
    showPreview: "Message Preview",
    muteFor1h: "Mute for 1 hour",
    muteFor8h: "Mute for 8 hours",
    muteFor24h: "Mute for 24 hours",
    muteForever: "Mute forever",
    unmute: "Unmute",
    mutedConversations: "Muted Conversations",
    noMutedConversations: "No muted conversations",
    blockedUsers: "Blocked users",
    unblock: "Unblock",
    desktopNotifications: "Desktop Notifications",
    notificationsEnable: "Enable notifications",
    notificationsGranted: "Notifications enabled",
    notificationsDenied: "Blocked by the browser — allow notifications in site settings",
    notificationsBlocked: "Blocked",
    previewOn: "Preview on",
    previewOff: "Preview off",
    profile: "Profile",
    appearance: "Appearance",
    notifications: "Notifications",
    data: "Data",
    account: "Account",
    themeLight: "Light",
    themeDark: "Dark",
    themeSystem: "System",
    testSound: "Test sound",
    exportData: "Export chat history",
    myAccount: "My Account",
    openSettings: "Open Settings",
    assistantSection: "Assistant",
    showReasoning: "Show reasoning",
    showReasoningHint: "Keep a collapsed trace above each answer",
    webSearchStatus: "Web search",
    ambientStatus: "Proactive replies",
    ambientHint: "TokenBot may join without an @ mention when it judges it useful",
    capabilityOn: "Enabled",
    capabilityOff: "Disabled",
    about: "About",
    language: "Language",
    accountSection: "Account",
    accountGuest: "Guest",
    accountMember: "Signed in",
  },
  slash: {
    me: "Send as action message",
    topic: "Change chat topic",
    shrug: "¯\\_(ツ)_/¯",
    tableflip: "(╯°□°)╯︵ ┻━┻",
  },
  thread: {
    replies: "replies",
    replyCount: "{count} replies",
    replyPlaceholder: "Reply to thread...",
    close: "Close",
  },
  editor: {
    bold: "Bold",
    italic: "Italic",
    strikethrough: "Strikethrough",
    code: "Code",
    quote: "Quote",
    link: "Link",
    preview: "Preview",
    linkUrl: "Enter URL",
    formatting: "Markdown formatting",
  },
  export: {
    exportJson: "Export as JSON",
    exportText: "Export as Text",
    exportSuccess: "Chat exported successfully",
    exportError: "Failed to export chat",
  },
  file: {
    uploading: "Uploading...",
    downloadFile: "Download file",
    fileSize: "{{size}}",
    dropFilesHere: "Drop files here",
  },
  poll: {
    closed: "Poll closed",
    finalResults: "Final Results",
    vote: "Vote",
    votes: "{{count}} votes",
  },
  a11y: {
    close: "Close",
    back: "Back",
    clearSearch: "Clear search",
    closeSidebar: "Close sidebar",
    openSidebar: "Open sidebar",
    collapseSidebar: "Collapse sidebar",
    expandSidebar: "Expand sidebar",
    toggleInfo: "Show or hide conversation info",
    closeInfo: "Close conversation info",
    moreActions: "More actions",
    addAttachment: "Add attachment",
    removeImage: "Remove image",
    ok: "OK",
    uploadFile: "Upload file",
    prevResult: "Previous result",
    nextResult: "Next result",
    closeSearch: "Close search",
    zoomOut: "Zoom out",
    zoomIn: "Zoom in",
    copyCode: "Copy code",
    audioSeek: "Audio seek",
    exitSelect: "Exit select mode",
    prevMonth: "Previous month",
    nextMonth: "Next month",
    hour: "Hour",
    minute: "Minute",
    scrollToBottom: "Scroll to bottom",
    online: "Online",
    showPassword: "Show password",
    hidePassword: "Hide password",
  },

};


export const translations: Record<Language, TranslationDict> = {
  "zh-CN": zhCN,
  "en-US": enUS,
};

export const STORAGE_KEY = STORAGE_KEYS.language;

export function detectLanguage(): Language {
  if (typeof window === "undefined") return "zh-CN";
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "zh-CN" || stored === "en-US") return stored;
  } catch {
    // localStorage may throw in restrictive environments (iframe sandbox,
    // private browsing in older browsers).  Fall through to navigator.
  }
  const navLang =
    navigator.language ||
    (navigator as { userLanguage?: string }).userLanguage ||
    "";
  if (navLang.startsWith("zh")) return "zh-CN";
  return "en-US";
}
