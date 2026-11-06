/* app.js — 助手原型交互层：会话管理、流式对话、全局语音、悬浮形象、设置与主题 */
;(function () {
  'use strict'

  const STORAGE_KEY = 'bk.assistant.proto.v1'
  const THEME_KEY = 'bk.assistant.proto.theme'

  const $ = (selector, root) => (root || document).querySelector(selector)
  const $$ = (selector, root) => Array.from((root || document).querySelectorAll(selector))
  const icon = (name, extra) =>
    '<svg class="icon' + (extra ? ' ' + extra : '') + '" aria-hidden="true"><use href="#' + name + '"></use></svg>'

  const html = document.documentElement
  const agentCard = $('#agentCard')
  const agentDragArea = $('#agentDragArea')
  const mascot = $('#mascot')
  const agentStatus = $('#agentCardStatus')
  const agentStatusLabel = $('#agentStatusLabel')
  const agentStatusText = $('#agentStatusText')
  const toolbar = $('#agentToolbar')
  const messageList = $('#messageList')
  const sessionList = $('#sessionList')
  const sessionPanel = $('#sessionPanel')
  const openPanelBtn = $('#openPanelBtn')
  const togglePanelBtn = $('#togglePanelBtn')
  const collapsePanelBtn = $('#collapsePanelBtn')
  const newSessionBtn = $('#newSessionBtn')
  const composerInput = $('#composerInput')
  const sendBtn = $('#sendBtn')
  const micBtn = $('#micBtn')
  const ttsBtn = $('#ttsBtn')
  const sendHint = $('#sendHint')
  const inlineVoice = $('#inlineVoice')
  const inlineVoiceText = $('#inlineVoiceText')
  const globalVoice = $('#globalVoice')
  const globalVoiceTitle = $('#globalVoiceTitle')
  const globalVoiceHint = $('#globalVoiceHint')
  const cancelGlobalVoice = $('#cancelGlobalVoice')
  const themeBtn = $('#themeBtn')
  const toggleAvatarBtn = $('#toggleAvatarBtn')
  const settingsBtns = [$('#settingsBtn'), $('#chatSettingsBtn')]
  const modalLayer = $('#modalLayer')
  const settingsModal = $('#settingsModal')
  const confirmModal = $('#confirmModal')
  const confirmTitle = $('#confirmTitle')
  const confirmMessage = $('#confirmMessage')
  const confirmActionBtn = $('#confirmActionBtn')
  const resetDemoBtn = $('#resetDemoBtn')
  const modelSegment = $('#modelSegment')
  const toastStack = $('#toastStack')
  const scrollBottomBtn = $('#scrollBottomBtn')

  let agent = { name: '小蝶', greeting: '欢迎回来。今天想先聊点什么？' }
  let suggestionList = []

  const state = {
    sessions: [],
    currentId: null,
    drafts: {},
    panelCollapsed: window.innerWidth < 1024,
    settings: {
      tts: true,
      voiceInput: true,
      memoryHints: true,
      reducedMotion: false,
      model: '小蝶 · 标准'
    },
    card: { visible: true, x: null, y: null }
  }

  let activeStream = null
  let streamToken = 0
  let renamingId = null
  let confirmHandler = null
  let moodTimer = null
  let actionTimer = null
  let saveTimer = null
  let suppressClickUntil = 0
  let composing = false
  let inlineRecording = false
  let inlineStartAt = 0
  let inlineStartY = 0
  let globalRecording = false
  let globalStartAt = 0
  let globalBusy = false
  let globalCancelled = false
  let globalTimeout = null
  let ttsSupported =
    typeof window.speechSynthesis !== 'undefined' && typeof window.SpeechSynthesisUtterance !== 'undefined'

  /* ------------------------------- 基础工具 ------------------------------- */

  function uid(prefix) {
    return prefix + '-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6)
  }

  function escapeHtml(value) {
    return String(value)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
  }

  function inlineMarkdown(text) {
    return text
      .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
      .replace(/`([^`]+)`/g, '<code>$1</code>')
  }

  function renderMarkdown(text) {
    const source = escapeHtml(text || '')
    const lines = source.split('\n')
    let html = ''
    let listType = null
    let paragraph = []

    const flushParagraph = () => {
      if (!paragraph.length) return
      html += '<p>' + paragraph.join('<br>') + '</p>'
      paragraph = []
    }
    const closeList = () => {
      if (!listType) return
      html += '</' + listType + '>'
      listType = null
    }

    lines.forEach((raw) => {
      const line = raw.trim()
      if (!line) {
        flushParagraph()
        closeList()
        return
      }
      if (/^&gt;\s?/.test(line)) {
        flushParagraph()
        closeList()
        html += '<blockquote>' + inlineMarkdown(line.replace(/^&gt;\s?/, '')) + '</blockquote>'
        return
      }
      const ordered = line.match(/^(\d+)\.\s+(.*)$/)
      if (ordered) {
        flushParagraph()
        if (listType !== 'ol') {
          closeList()
          html += '<ol>'
          listType = 'ol'
        }
        html += '<li>' + inlineMarkdown(ordered[2]) + '</li>'
        return
      }
      const bullet = line.match(/^[-*]\s+(.*)$/)
      if (bullet) {
        flushParagraph()
        if (listType !== 'ul') {
          closeList()
          html += '<ul>'
          listType = 'ul'
        }
        html += '<li>' + inlineMarkdown(bullet[1]) + '</li>'
        return
      }
      closeList()
      paragraph.push(inlineMarkdown(line))
    })

    flushParagraph()
    closeList()
    return html
  }

  function nowTime() {
    return new Date().toLocaleTimeString('zh-CN', {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    })
  }

  function snippet(text) {
    const flat = String(text || '')
      .replace(/[#>*`]/g, '')
      .replace(/\s+/g, ' ')
      .trim()
    return flat.length > 42 ? flat.slice(0, 42) + '…' : flat
  }

  function makeTitle(text) {
    const flat = String(text || '')
      .replace(/[\r\n]+/g, ' ')
      .replace(/[，。！？、；：,.!?;:]/g, ' ')
      .replace(/\s+/g, ' ')
      .trim()
    return flat.length > 14 ? flat.slice(0, 14) + '…' : flat
  }

  function readJSON(key) {
    try {
      const raw = localStorage.getItem(key)
      return raw ? JSON.parse(raw) : null
    } catch (error) {
      return null
    }
  }

  function toast(text, kind) {
    const node = document.createElement('div')
    node.className = 'toast' + (kind === 'error' ? ' is-error' : kind === 'success' ? ' is-success' : '')
    node.innerHTML = icon(kind === 'error' ? 'i-x' : 'i-check') + '<span>' + escapeHtml(text) + '</span>'
    toastStack.appendChild(node)
    window.setTimeout(() => {
      node.classList.add('is-leaving')
      window.setTimeout(() => node.remove(), 220)
    }, 2400)
  }

  /* ------------------------------- 会话状态 ------------------------------- */

  function sessionById(id) {
    return state.sessions.find((item) => item.id === id) || null
  }

  function currentSession() {
    return sessionById(state.currentId)
  }

  function isCurrent(session) {
    return !!session && session.id === state.currentId
  }

  function bumpSession(session) {
    const index = state.sessions.indexOf(session)
    if (index > 0) {
      state.sessions.splice(index, 1)
      state.sessions.unshift(session)
    }
  }

  function saveState() {
    window.clearTimeout(saveTimer)
    saveTimer = window.setTimeout(() => {
      try {
        localStorage.setItem(
          STORAGE_KEY,
          JSON.stringify({
            v: 2,
            sessions: state.sessions,
            currentId: state.currentId,
            drafts: state.drafts,
            panelCollapsed: state.panelCollapsed,
            settings: state.settings,
            card: state.card
          })
        )
      } catch (error) {
        /* 本地存储不可用时静默降级 */
      }
    }, 150)
  }

  function normalizeSessions(list) {
    if (!Array.isArray(list)) return []
    return list
      .filter((item) => item && typeof item.id === 'string' && Array.isArray(item.messages))
      .map((item) => {
        item.messages.forEach((message) => {
          if (message.status === 'streaming') message.status = 'done'
        })
        return item
      })
  }

  /* ------------------------------- 渲染：会话列表 ------------------------------- */

  function renderSessionList() {
    sessionList.innerHTML = ''
    if (!state.sessions.length) return

    state.sessions.forEach((session) => {
      const active = session.id === state.currentId
      const item = document.createElement('div')
      item.className = 'session-item' + (active ? ' is-active' : '')
      item.dataset.id = session.id
      item.setAttribute('role', 'option')
      item.setAttribute('aria-selected', String(active))
      item.tabIndex = 0

      if (renamingId === session.id) {
        const input = document.createElement('input')
        input.className = 'session-rename'
        input.type = 'text'
        input.maxLength = 40
        input.value = session.title
        input.setAttribute('aria-label', '会话标题')

        let settled = false
        const commit = async () => {
          if (settled) return
          settled = true
          const next = input.value.trim()
          if (!next) {
            toast('会话标题不能为空', 'error')
            settled = false
            input.focus()
            return
          }
          if (next === session.title) {
            renamingId = null
            renderSessionList()
            return
          }
          try {
            await window.API.renameSession({ id: session.id, title: next })
            session.title = next
            session.autoTitle = false
            renamingId = null
            renderSessionList()
            saveState()
            toast('会话已重命名', 'success')
          } catch (error) {
            renamingId = null
            renderSessionList()
            toast(error.message || '重命名失败', 'error')
          }
        }
        input.addEventListener('keydown', (event) => {
          if (event.key === 'Enter') {
            event.preventDefault()
            commit()
          }
          if (event.key === 'Escape') {
            event.preventDefault()
            settled = true
            renamingId = null
            renderSessionList()
          }
        })
        input.addEventListener('blur', commit)
        item.appendChild(input)
        window.setTimeout(() => {
          input.focus()
          input.select()
        }, 0)
      } else {
        item.innerHTML =
          '<div class="session-item-top">' +
          '<span class="session-title">' +
          escapeHtml(session.title) +
          '</span>' +
          '<span class="session-time">' +
          escapeHtml(session.updatedAt || '') +
          '</span>' +
          '</div>' +
          '<span class="session-preview">' +
          escapeHtml(session.preview || '还没有消息') +
          '</span>' +
          '<div class="session-item-actions">' +
          '<button class="session-action" type="button" data-act="rename" title="重命名" aria-label="重命名会话">' +
          icon('i-pencil') +
          '</button>' +
          '<button class="session-action is-danger" type="button" data-act="delete" title="删除" aria-label="删除会话">' +
          icon('i-trash') +
          '</button>' +
          '</div>'
      }

      sessionList.appendChild(item)
    })
  }

  /* ------------------------------- 渲染：消息 ------------------------------- */

  function messageActionButton(op, iconName, label) {
    return (
      '<button class="message-action" type="button" data-op="' +
      op +
      '" title="' +
      label +
      '" aria-label="' +
      label +
      '">' +
      icon(iconName) +
      '<span>' +
      label +
      '</span></button>'
    )
  }

  function buildMessageEl(message) {
    const isUser = message.role === 'user'
    const node = document.createElement('div')
    node.className = 'message ' + (isUser ? 'is-user' : 'is-agent')
    node.dataset.id = message.id

    const avatar = document.createElement('div')
    avatar.className = 'message-avatar' + (isUser ? ' is-user' : '')
    if (isUser) {
      avatar.innerHTML = '<span aria-hidden="true">未</span>'
    } else {
      avatar.innerHTML = icon('i-bow')
    }

    const wrap = document.createElement('div')
    wrap.className = 'bubble-wrap'

    const meta = document.createElement('span')
    meta.className = 'bubble-meta'
    meta.textContent = (isUser ? '我' : agent.name) + ' · ' + (message.time || '')

    const bubble = document.createElement('div')
    bubble.className = 'bubble'
    bubble.innerHTML =
      renderMarkdown(message.content) + (message.status === 'streaming' ? '<span class="stream-caret"></span>' : '')

    wrap.appendChild(meta)
    wrap.appendChild(bubble)

    if (message.memoryWritten && state.settings.memoryHints) {
      const memory = document.createElement('span')
      memory.className = 'message-memory'
      memory.innerHTML = icon('i-check') + '<span>已写入长期记忆</span>'
      wrap.appendChild(memory)
    }

    if (message.status !== 'streaming') {
      const actions = document.createElement('div')
      actions.className = 'message-actions'
      let markup = messageActionButton('copy', 'i-copy', '复制')
      if (isUser) {
        markup += messageActionButton('delete', 'i-trash', '删除')
      } else {
        markup += messageActionButton('regenerate', 'i-refresh', '重答')
        markup += messageActionButton('speak', 'i-volume', '播报')
      }
      actions.innerHTML = markup
      wrap.appendChild(actions)
    }

    node.appendChild(avatar)
    node.appendChild(wrap)
    return node
  }

  function dateDividerLabel(session) {
    if (session.updatedAt === '刚刚' || session.updatedAt === '2 小时前') return '今天'
    if (session.updatedAt === '昨天') return '昨天'
    return '历史会话'
  }

  function buildEmptyState() {
    const wrap = document.createElement('div')
    wrap.className = 'empty-state'
    wrap.innerHTML =
      '<span class="empty-mark">' +
      icon('i-sparkles') +
      '</span>' +
      '<h2>开始一段新的对话</h2>' +
      '<p>' +
      escapeHtml(agent.greeting || '欢迎回来。今天想先聊点什么？') +
      '我会记住不同会话里聊过的事，也会在需要时把它们连起来。</p>'

    if (suggestionList.length) {
      const row = document.createElement('div')
      row.className = 'suggestion-row'
      suggestionList.forEach((text) => {
        const chip = document.createElement('button')
        chip.className = 'suggestion-chip'
        chip.type = 'button'
        chip.dataset.text = text
        chip.textContent = text
        row.appendChild(chip)
      })
      wrap.appendChild(row)
    }
    return wrap
  }

  function renderThread() {
    const session = currentSession()
    messageList.innerHTML = ''

    if (!session || !session.messages.length) {
      messageList.appendChild(buildEmptyState())
      updateScrollButton()
      return
    }

    const divider = document.createElement('div')
    divider.className = 'date-divider'
    divider.textContent = dateDividerLabel(session)
    messageList.appendChild(divider)

    session.messages.forEach((message) => {
      messageList.appendChild(buildMessageEl(message))
    })
    updateScrollButton()
  }

  function updateScrollButton() {
    scrollBottomBtn.hidden = isNearBottom()
  }

  function isNearBottom() {
    return messageList.scrollHeight - messageList.scrollTop - messageList.clientHeight < 90
  }

  function scrollToBottom(force) {
    if (!force && !isNearBottom()) return
    messageList.scrollTop = messageList.scrollHeight
    updateScrollButton()
  }

  function restoreDraft() {
    composerInput.value = state.drafts[state.currentId] || ''
    autosizeInput()
  }

  function renderAll() {
    renderSessionList()
    renderThread()
    restoreDraft()
  }

  /* ------------------------------- 会话操作 ------------------------------- */

  function setPanelCollapsed(collapsed) {
    state.panelCollapsed = !!collapsed
    sessionPanel.classList.toggle('is-collapsed', state.panelCollapsed)
    openPanelBtn.hidden = !state.panelCollapsed
    const label = state.panelCollapsed ? '展开会话面板' : '收起会话面板'
    togglePanelBtn.title = label
    togglePanelBtn.setAttribute('aria-label', label)
    saveState()
  }

  function switchSession(id) {
    if (!id || id === state.currentId) return
    if (state.currentId) state.drafts[state.currentId] = composerInput.value
    stopActiveStream()
    stopTts()
    renamingId = null
    state.currentId = id
    renderAll()
    scrollToBottom(true)
    composerInput.focus()
    saveState()
  }

  async function createSession() {
    if (state.currentId) state.drafts[state.currentId] = composerInput.value
    stopActiveStream()
    const now = new Date()
    const title = '新会话 · ' + String(now.getMonth() + 1).padStart(2, '0') + '/' + String(now.getDate()).padStart(2, '0')
    const created = await window.API.createSession({ title })
    created.autoTitle = true
    state.sessions.unshift(created)
    state.currentId = created.id
    state.drafts[created.id] = ''
    renamingId = null
    renderAll()
    scrollToBottom(true)
    composerInput.focus()
    saveState()
  }

  function requestDelete(session) {
    if (state.sessions.length <= 1) {
      toast('至少保留一个会话', 'error')
      return
    }
    openConfirm({
      title: '删除会话',
      message: '确定删除「' + session.title + '」吗？删除后将无法恢复。',
      confirmText: '删除',
      onConfirm: async () => {
        try {
          await window.API.deleteSession({ id: session.id })
        } catch (error) {
          /* 桩层失败不影响本地演示 */
        }
        const index = state.sessions.indexOf(session)
        state.sessions = state.sessions.filter((item) => item.id !== session.id)
        delete state.drafts[session.id]
        if (state.currentId === session.id) {
          const next = state.sessions[Math.min(index, state.sessions.length - 1)]
          if (activeStream && activeStream.session.id === session.id) stopActiveStream()
          state.currentId = next ? next.id : null
          renderAll()
          scrollToBottom(true)
        } else {
          renderSessionList()
        }
        saveState()
        toast('会话已删除', 'success')
      }
    })
  }

  /* ------------------------------- 流式对话 ------------------------------- */

  function buildTypingRow() {
    const row = document.createElement('div')
    row.className = 'typing-row'
    row.innerHTML =
      '<div class="message-avatar">' +
      icon('i-bow') +
      '</div><div class="typing-bubble"><span></span><span></span><span></span></div>'
    return row
  }

  function syncComposerBusy() {
    const busy = !!activeStream
    sendBtn.innerHTML = icon(busy ? 'i-stop' : 'i-send')
    sendBtn.title = busy ? '停止生成' : '发送'
    sendBtn.setAttribute('aria-label', busy ? '停止生成' : '发送消息')
  }

  async function streamReply(session, sourceText, options) {
    stopActiveStream()
    const opts = options || {}
    const token = ++streamToken
    const controller = new AbortController()
    const agentMessage = {
      id: uid('a'),
      role: 'agent',
      content: '',
      time: nowTime(),
      status: 'streaming',
      emotion: 'neutral',
      action: 'idle',
      memoryWritten: false
    }
    const ctx = {
      token: token,
      session: session,
      controller: controller,
      agentMessage: agentMessage,
      node: null,
      typingEl: null,
      finished: false,
      forceSpeak: !!opts.forceSpeak
    }
    activeStream = ctx

    if (isCurrent(session)) {
      ctx.typingEl = buildTypingRow()
      messageList.appendChild(ctx.typingEl)
      scrollToBottom(true)
      setStatus('thinking')
    }
    syncComposerBusy()

    let result
    try {
      result = await window.API.sendMessage({
        text: sourceText,
        signal: controller.signal,
        onChunk: function (buffer) {
          if (ctx.finished) return
          ctx.agentMessage.content = buffer
          if (!isCurrent(session)) return
          if (!ctx.node) {
            if (ctx.typingEl) {
              ctx.typingEl.remove()
              ctx.typingEl = null
            }
            ctx.node = buildMessageEl(ctx.agentMessage)
            messageList.appendChild(ctx.node)
          }
          const bubble = ctx.node.querySelector('.bubble')
          if (bubble) {
            bubble.innerHTML = renderMarkdown(buffer) + '<span class="stream-caret"></span>'
          }
          if (isNearBottom()) scrollToBottom()
        }
      })
    } catch (error) {
      result = {
        aborted: true,
        content: ctx.agentMessage.content,
        emotion: 'neutral',
        action: 'idle',
        memory: false
      }
    }

    if (ctx.finished) return
    finishStream(ctx, result)
  }

  function finishStream(ctx, result) {
    ctx.finished = true
    if (activeStream === ctx) activeStream = null
    if (ctx.typingEl) {
      ctx.typingEl.remove()
      ctx.typingEl = null
    }

    const session = ctx.session
    const message = ctx.agentMessage
    const aborted = !!(result && result.aborted)
    const content = (result && result.content) || message.content || ''
    let appended = false

    if (content.trim()) {
      message.content = content
      message.status = 'done'
      message.emotion = (result && result.emotion) || 'neutral'
      message.action = (result && result.action) || 'idle'
      message.memoryWritten = !!(result && result.memory)
      if (session.messages.indexOf(message) === -1) session.messages.push(message)
      session.preview = snippet(content)
      session.updatedAt = '刚刚'
      bumpSession(session)
      appended = true
    }

    syncComposerBusy()
    if (!renamingId) renderSessionList()

    if (isCurrent(session)) {
      renderThread()
      scrollToBottom(false)
      setStatus('idle')
      setMood(message.emotion)
      if (!aborted) playAction(message.action)
    } else {
      setStatus('idle')
    }

    saveState()

    if (appended && !aborted && content.trim()) {
      speak(content, ctx.forceSpeak)
    }
    if (aborted) setMood('neutral')
  }

  function stopActiveStream() {
    const ctx = activeStream
    if (!ctx) return
    activeStream = null
    ctx.controller.abort()
    if (!ctx.finished) {
      finishStream(ctx, {
        aborted: true,
        content: ctx.agentMessage.content,
        emotion: 'neutral',
        action: 'idle',
        memory: false
      })
    }
  }

  async function sendMessage(text, options) {
    const content = String(text || '').trim()
    if (!content) return
    const session = currentSession()
    if (!session) return

    stopActiveStream()
    stopTts()
    setMood('neutral')

    const userMessage = {
      id: uid('u'),
      role: 'user',
      content: content,
      time: nowTime(),
      status: 'done'
    }
    session.messages.push(userMessage)
    if (session.autoTitle && session.messages.filter((item) => item.role === 'user').length === 1) {
      const autoTitle = makeTitle(content)
      if (autoTitle) {
        session.title = autoTitle
        session.autoTitle = false
      }
    }
    session.preview = snippet(content)
    session.updatedAt = '刚刚'
    bumpSession(session)

    state.drafts[session.id] = ''
    composerInput.value = ''
    autosizeInput()
    renderSessionList()
    renderThread()
    scrollToBottom(true)
    saveState()

    await streamReply(session, content, options || {})
  }

  function submitComposer() {
    const text = composerInput.value
    if (!text.trim()) return
    sendMessage(text)
  }

  /* ------------------------------- 消息操作 ------------------------------- */

  function findMessage(id) {
    const session = currentSession()
    if (!session) return null
    const message = session.messages.find((item) => item.id === id)
    return message ? { session: session, message: message } : null
  }

  function copyText(text) {
    const fallback = () => {
      const area = document.createElement('textarea')
      area.value = text
      area.setAttribute('readonly', '')
      area.style.position = 'fixed'
      area.style.opacity = '0'
      document.body.appendChild(area)
      area.select()
      try {
        document.execCommand('copy')
        toast('已复制到剪贴板', 'success')
      } catch (error) {
        toast('复制失败，请手动选择文本', 'error')
      }
      area.remove()
    }

    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(() => toast('已复制到剪贴板', 'success'), fallback)
      return
    }
    fallback()
  }

  async function regenerate(message) {
    const session = currentSession()
    if (!session) return
    const index = session.messages.findIndex((item) => item.id === message.id)
    if (index < 0) return
    if (index !== session.messages.length - 1) {
      toast('只能重新生成最后一条回复', 'error')
      return
    }
    let source = ''
    for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
      if (session.messages[cursor].role === 'user') {
        source = session.messages[cursor].content
        break
      }
    }
    if (!source) {
      toast('没有找到对应的提问', 'error')
      return
    }
    stopTts()
    stopActiveStream()
    session.messages.splice(index, 1)
    renderThread()
    renderSessionList()
    await streamReply(session, source)
  }

  function deleteMessage(message) {
    const session = currentSession()
    if (!session) return
    const index = session.messages.findIndex((item) => item.id === message.id)
    if (index < 0) return
    session.messages.splice(index, 1)
    const last = session.messages[session.messages.length - 1]
    session.preview = last ? snippet(last.content) : '还没有消息'
    renderThread()
    renderSessionList()
    saveState()
    toast('消息已删除', 'success')
  }

  /* ------------------------------- 语音播报 ------------------------------- */

  function pickVoice() {
    if (!ttsSupported) return null
    const voices = window.speechSynthesis.getVoices() || []
    return (
      voices.find((item) => /zh|cmn/i.test(item.lang) && /female|女|Xiaoxiao|Yaoyao/i.test(item.name)) ||
      voices.find((item) => /zh|cmn/i.test(item.lang)) ||
      null
    )
  }

  function stopTts() {
    if (!ttsSupported) return
    window.speechSynthesis.cancel()
    mascot.classList.remove('is-speaking')
    if (!activeStream) setStatus('idle')
  }

  function speak(text, force) {
    if (!force && !state.settings.tts) return
    if (!ttsSupported) {
      if (force) toast('当前浏览器不支持语音播报', 'error')
      return
    }
    const clean = String(text || '')
      .replace(/[#>*`]/g, '')
      .replace(/\n+/g, '，')
      .trim()
    if (!clean) return

    window.speechSynthesis.cancel()
    const utterance = new window.SpeechSynthesisUtterance(clean)
    utterance.lang = 'zh-CN'
    utterance.rate = 1.02
    utterance.pitch = 1.05
    const voice = pickVoice()
    if (voice) utterance.voice = voice

    utterance.onstart = () => setStatus('speaking')
    utterance.onend = () => {
      if (!activeStream) setStatus('idle')
    }
    utterance.onerror = () => {
      if (!activeStream) setStatus('idle')
    }
    try {
      window.speechSynthesis.speak(utterance)
    } catch (error) {
      /* 部分浏览器限制自动播放，静默降级 */
    }
  }

  /* ------------------------------- 形象卡片 ------------------------------- */

  function clampCard(x, y) {
    const width = agentCard.offsetWidth || 196
    const height = agentCard.offsetHeight || 274
    const margin = 8
    return {
      x: Math.min(Math.max(margin, x), Math.max(margin, window.innerWidth - width - margin)),
      y: Math.min(Math.max(margin, y), Math.max(margin, window.innerHeight - height - margin))
    }
  }

  function placeCard(x, y) {
    const point = clampCard(x, y)
    agentCard.style.left = point.x + 'px'
    agentCard.style.top = point.y + 'px'
    return point
  }

  function snapCard() {
    const rect = agentCard.getBoundingClientRect()
    const threshold = 32
    let x = rect.left
    let y = rect.top
    if (x < threshold) x = 10
    else if (window.innerWidth - (x + rect.width) < threshold) x = window.innerWidth - rect.width - 10
    if (y < threshold) y = 10
    else if (window.innerHeight - (y + rect.height) < threshold) y = window.innerHeight - rect.height - 10
    return placeCard(x, y)
  }

  function setCardVisible(visible) {
    state.card.visible = !!visible
    agentCard.classList.toggle('is-hidden', !state.card.visible)
    toggleAvatarBtn.classList.toggle('is-on', state.card.visible)
    toggleAvatarBtn.setAttribute('aria-pressed', String(state.card.visible))
    toggleAvatarBtn.innerHTML = icon(state.card.visible ? 'i-eye' : 'i-eye-off') + '<span>形象</span>'
    syncSettingsUI()
    saveState()
  }

  function setMood(mood) {
    mascot.dataset.mood = mood || 'neutral'
    window.clearTimeout(moodTimer)
    if (mood && mood !== 'neutral') {
      moodTimer = window.setTimeout(() => {
        mascot.dataset.mood = 'neutral'
      }, 6000)
    }
  }

  function playAction(action) {
    const names = ['wave', 'nod', 'dance', 'angry']
    window.clearTimeout(actionTimer)
    names.forEach((name) => mascot.classList.remove('action-' + name))
    if (!action || action === 'idle' || names.indexOf(action) === -1) return
    void mascot.offsetWidth
    mascot.classList.add('action-' + action)
    if (action === 'angry') setMood('angry')
    actionTimer = window.setTimeout(() => {
      mascot.classList.remove('action-' + action)
    }, 1800)
  }

  function setStatus(mode) {
    const labels = {
      idle: '待机 · 在线',
      listening: '正在聆听',
      thinking: '思考中',
      speaking: '播报中'
    }
    const text = labels[mode] || labels.idle
    agentStatusLabel.textContent = text
    agentStatusText.textContent = mode === 'idle' ? '在线 · 主 Agent' : text
    agentStatus.classList.toggle('is-listening', mode === 'listening')
    agentStatus.classList.toggle('is-thinking', mode === 'thinking')
    agentStatus.classList.toggle('is-speaking', mode === 'speaking')
    mascot.classList.toggle('is-speaking', mode === 'speaking')
  }

  /* ------------------------------- 全局语音 ------------------------------- */

  function startGlobalVoice() {
    if (globalRecording || globalBusy) return
    globalRecording = true
    globalCancelled = false
    globalStartAt = performance.now()
    globalVoice.hidden = false
    globalVoiceTitle.textContent = '正在聆听'
    globalVoiceHint.textContent = '松开 Ctrl + Space 发送'
    setStatus('listening')
    setMood('neutral')
    window.clearTimeout(globalTimeout)
    globalTimeout = window.setTimeout(() => {
      if (globalRecording) stopGlobalVoice(false)
    }, 15000)
  }

  async function stopGlobalVoice(cancelled) {
    if (!globalRecording) return
    globalRecording = false
    window.clearTimeout(globalTimeout)
    const duration = (performance.now() - globalStartAt) / 1000

    if (cancelled) {
      globalVoice.hidden = true
      setStatus('idle')
      return
    }

    globalBusy = true
    globalCancelled = false
    globalVoiceTitle.textContent = '正在识别'
    globalVoiceHint.textContent = '马上就好'
    const result = await window.API.transcribe({ duration: duration })
    globalBusy = false
    if (globalCancelled) {
      globalCancelled = false
      globalVoice.hidden = true
      setStatus('idle')
      return
    }
    globalVoice.hidden = true

    if (!result.text) {
      setStatus('idle')
      toast(result.error || '没有听清，再试一次', 'error')
      return
    }

    toast('语音已发送', 'success')
    await sendMessage(result.text, { forceSpeak: true })
  }

  /* ------------------------------- 设置与主题 ------------------------------- */

  function syncSettingsUI() {
    $$('.switch[data-setting]').forEach((button) => {
      const key = button.dataset.setting
      const on = key === 'cardVisible' ? state.card.visible : !!state.settings[key]
      button.classList.toggle('is-on', on)
      button.setAttribute('aria-checked', String(on))
    })

    ttsBtn.classList.toggle('is-on', state.settings.tts)
    ttsBtn.innerHTML = icon(state.settings.tts ? 'i-volume' : 'i-volume-off')
    ttsBtn.setAttribute('aria-pressed', String(state.settings.tts))
    micBtn.hidden = !state.settings.voiceInput

    $$('button', modelSegment).forEach((button) => {
      button.classList.toggle('is-selected', button.dataset.model === state.settings.model)
      button.setAttribute('aria-checked', String(button.dataset.model === state.settings.model))
    })

    html.classList.toggle('reduce-motion', state.settings.reducedMotion)
  }

  function applyTheme(mode) {
    const next = mode === 'dark' ? 'dark' : 'light'
    html.dataset.theme = next
    themeBtn.innerHTML = icon(next === 'dark' ? 'i-sun' : 'i-moon')
    themeBtn.title = next === 'dark' ? '切换到浅色主题' : '切换到深色主题'
    try {
      localStorage.setItem(THEME_KEY, next)
    } catch (error) {
      /* 忽略存储失败 */
    }
  }

  function openModal(kind) {
    modalLayer.hidden = false
    settingsModal.hidden = kind !== 'settings'
    confirmModal.hidden = kind !== 'confirm'
  }

  function closeModal() {
    modalLayer.hidden = true
    settingsModal.hidden = true
    confirmModal.hidden = true
    confirmHandler = null
  }

  function openConfirm(options) {
    confirmTitle.textContent = options.title || '请确认'
    confirmMessage.textContent = options.message || ''
    confirmActionBtn.textContent = options.confirmText || '确定'
    confirmHandler = options.onConfirm || null
    openModal('confirm')
  }

  /* ------------------------------- 事件绑定 ------------------------------- */

  function autosizeInput() {
    composerInput.style.height = 'auto'
    composerInput.style.height = Math.min(composerInput.scrollHeight, 150) + 'px'
  }

  function bindEvents() {
    toggleAvatarBtn.addEventListener('click', () => setCardVisible(!state.card.visible))

    themeBtn.addEventListener('click', () => {
      applyTheme(html.dataset.theme === 'dark' ? 'light' : 'dark')
    })

    settingsBtns.forEach((button) => {
      if (!button) return
      button.addEventListener('click', () => {
        syncSettingsUI()
        openModal('settings')
      })
    })

    $$('[data-close-modal]').forEach((button) => {
      button.addEventListener('click', closeModal)
    })

    confirmActionBtn.addEventListener('click', () => {
      const handler = confirmHandler
      closeModal()
      if (handler) handler()
    })

    resetDemoBtn.addEventListener('click', () => {
      openConfirm({
        title: '重置演示数据',
        message: '将清除本地会话、设置与形象位置，并恢复到初始演示状态。',
        confirmText: '重置',
        onConfirm: () => {
          try {
            localStorage.removeItem(STORAGE_KEY)
            localStorage.removeItem(THEME_KEY)
          } catch (error) {
            /* 忽略存储失败 */
          }
          window.location.reload()
        }
      })
    })

    togglePanelBtn.addEventListener('click', () => setPanelCollapsed(!state.panelCollapsed))
    collapsePanelBtn.addEventListener('click', () => setPanelCollapsed(true))
    openPanelBtn.addEventListener('click', () => setPanelCollapsed(false))

    newSessionBtn.addEventListener('click', () => {
      createSession().catch(() => toast('新建会话失败', 'error'))
    })

    sessionList.addEventListener('click', (event) => {
      const actionButton = event.target.closest('.session-action')
      if (actionButton) {
        event.stopPropagation()
        const item = actionButton.closest('.session-item')
        const session = item ? sessionById(item.dataset.id) : null
        if (!session) return
        if (actionButton.dataset.act === 'rename') {
          renamingId = session.id
          renderSessionList()
        } else if (actionButton.dataset.act === 'delete') {
          requestDelete(session)
        }
        return
      }
      const item = event.target.closest('.session-item')
      if (item) switchSession(item.dataset.id)
    })

    sessionList.addEventListener('keydown', (event) => {
      if (event.key !== 'Enter' && event.key !== ' ') return
      const item = event.target.closest('.session-item')
      if (!item || event.target.closest('.session-action') || event.target.classList.contains('session-rename')) return
      event.preventDefault()
      switchSession(item.dataset.id)
    })

    $$('.switch[data-setting]').forEach((button) => {
      button.addEventListener('click', () => {
        const key = button.dataset.setting
        if (key === 'cardVisible') {
          setCardVisible(!state.card.visible)
          return
        }
        if (key === 'memoryHints') {
          state.settings.memoryHints = !state.settings.memoryHints
          renderThread()
        } else if (key === 'reducedMotion') {
          state.settings.reducedMotion = !state.settings.reducedMotion
        } else {
          state.settings[key] = !state.settings[key]
          if (key === 'tts' && !state.settings.tts) stopTts()
        }
        syncSettingsUI()
        saveState()
      })
    })

    modelSegment.addEventListener('click', (event) => {
      const button = event.target.closest('button[data-model]')
      if (!button) return
      state.settings.model = button.dataset.model
      syncSettingsUI()
      saveState()
      toast('模型已切换为 ' + state.settings.model, 'success')
    })

    composerInput.addEventListener('input', () => {
      autosizeInput()
      state.drafts[state.currentId] = composerInput.value
    })

    composerInput.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && !composing) {
        event.preventDefault()
        submitComposer()
      }
    })

    composerInput.addEventListener('compositionstart', () => {
      composing = true
    })
    composerInput.addEventListener('compositionend', () => {
      composing = false
    })

    sendBtn.addEventListener('click', () => {
      if (activeStream) {
        stopActiveStream()
        return
      }
      submitComposer()
    })

    ttsBtn.addEventListener('click', () => {
      state.settings.tts = !state.settings.tts
      if (!state.settings.tts) stopTts()
      syncSettingsUI()
      saveState()
      toast(state.settings.tts ? '已开启语音播报' : '已关闭语音播报')
    })

    micBtn.addEventListener('pointerdown', (event) => {
      if (!state.settings.voiceInput || activeStream) return
      event.preventDefault()
      startInlineVoice(event.clientY)
    })

    window.addEventListener('pointerup', () => {
      if (inlineRecording) finishInlineVoice()
    })

    window.addEventListener('pointercancel', () => {
      if (inlineRecording) finishInlineVoice()
    })

    window.addEventListener('pointermove', (event) => {
      if (!inlineRecording) return
      if (event.clientY < inlineStartY - 70) {
        inlineRecording = false
        inlineVoice.hidden = true
        setStatus('idle')
        toast('已取消语音输入')
      }
    })

    messageList.addEventListener('click', (event) => {
      const chip = event.target.closest('.suggestion-chip')
      if (chip) {
        sendMessage(chip.dataset.text || chip.textContent || '')
        return
      }
      const actionButton = event.target.closest('.message-action')
      if (!actionButton) return
      const messageEl = actionButton.closest('.message')
      const found = messageEl ? findMessage(messageEl.dataset.id) : null
      if (!found) return
      const op = actionButton.dataset.op
      if (op === 'copy') copyText(found.message.content)
      else if (op === 'speak') speak(found.message.content, true)
      else if (op === 'regenerate') regenerate(found.message).catch(() => toast('重新生成失败', 'error'))
      else if (op === 'delete') deleteMessage(found.message)
    })

    messageList.addEventListener('scroll', updateScrollButton)
    scrollBottomBtn.addEventListener('click', () => scrollToBottom(true))

    agentDragArea.addEventListener('pointerdown', (event) => {
      if (event.button !== 0) return
      if (event.target.closest('.agent-toolbar')) return
      const rect = agentCard.getBoundingClientRect()
      agentCard.classList.add('is-dragging')
      agentDragArea._dragState = {
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        baseX: rect.left,
        baseY: rect.top,
        moved: false
      }
    })

    window.addEventListener('pointermove', (event) => {
      const drag = agentDragArea._dragState
      if (!drag || drag.pointerId !== event.pointerId) return
      const dx = event.clientX - drag.startX
      const dy = event.clientY - drag.startY
      if (!drag.moved && Math.abs(dx) + Math.abs(dy) < 4) return
      drag.moved = true
      placeCard(drag.baseX + dx, drag.baseY + dy)
    })

    const endDrag = (event) => {
      const drag = agentDragArea._dragState
      if (!drag || drag.pointerId !== event.pointerId) return
      if (drag.moved) {
        const point = snapCard()
        state.card.x = point.x
        state.card.y = point.y
        suppressClickUntil = Date.now() + 180
        saveState()
      }
      agentCard.classList.remove('is-dragging')
      agentDragArea._dragState = null
    }
    window.addEventListener('pointerup', endDrag)
    window.addEventListener('pointercancel', endDrag)

    mascot.addEventListener('click', (event) => {
      if (Date.now() < suppressClickUntil) return
      const hit = event.target.closest('[data-hit]')
      if (!hit) return
      if (hit.dataset.hit === 'face') {
        setMood('shy')
        playAction('nod')
        setStatus('idle')
      } else {
        setMood('happy')
        playAction('wave')
        toast(agent.name + '朝你挥了挥手')
      }
    })

    toolbar.addEventListener('click', (event) => {
      const button = event.target.closest('.tool-btn')
      if (!button) return
      const action = button.dataset.action
      if (action === 'hide') {
        setCardVisible(false)
        toast('已隐藏悬浮形象，可在顶部栏重新显示')
        return
      }
      if (action === 'angry') {
        setMood('angry')
        playAction('angry')
        return
      }
      setMood('happy')
      playAction(action)
    })

    $$('.nav-item').forEach((button) => {
      button.addEventListener('click', () => {
        if (button.dataset.nav === 'assistant') return
        toast('原型仅包含助手模块，其他模块暂未展开')
      })
    })

    cancelGlobalVoice.addEventListener('click', () => {
      if (globalBusy) {
        globalCancelled = true
        globalVoice.hidden = true
        setStatus('idle')
        return
      }
      stopGlobalVoice(true)
    })

    window.addEventListener('keydown', (event) => {
      if (event.code === 'Space' && (event.ctrlKey || event.metaKey)) {
        if (composing || event.isComposing) return
        event.preventDefault()
        if (!event.repeat) startGlobalVoice()
        return
      }
      if (event.key === 'Escape') {
        if (!modalLayer.hidden) {
          closeModal()
          return
        }
        if (activeStream) stopActiveStream()
      }
    })

    window.addEventListener('keyup', (event) => {
      if (event.code === 'Space' && globalRecording) {
        event.preventDefault()
        stopGlobalVoice(false)
      }
    })

    window.addEventListener('blur', () => {
      if (globalRecording) stopGlobalVoice(true)
      if (inlineRecording) finishInlineVoice()
    })

    window.addEventListener('resize', () => {
      if (state.card.x !== null && state.card.y !== null) {
        const point = placeCard(state.card.x, state.card.y)
        state.card.x = point.x
        state.card.y = point.y
      }
      updateScrollButton()
    })
  }

  /* ------------------------------- 输入区语音 ------------------------------- */

  function startInlineVoice(clientY) {
    inlineRecording = true
    inlineStartAt = performance.now()
    inlineStartY = typeof clientY === 'number' ? clientY : window.innerHeight
    inlineVoice.hidden = false
    inlineVoiceText.textContent = '正在聆听…'
    setStatus('listening')
  }

  async function finishInlineVoice() {
    if (!inlineRecording) return
    inlineRecording = false
    const duration = (performance.now() - inlineStartAt) / 1000
    inlineVoiceText.textContent = '正在识别…'
    const result = await window.API.transcribe({ duration: duration })
    inlineVoice.hidden = true
    setStatus('idle')
    if (!result.text) {
      toast(result.error || '没有听清，再试一次', 'error')
      return
    }
    composerInput.value = result.text
    state.drafts[state.currentId] = result.text
    autosizeInput()
    composerInput.focus()
  }

  /* ------------------------------- 初始化 ------------------------------- */

  async function init() {
    const savedTheme = (function () {
      try {
        return localStorage.getItem(THEME_KEY)
      } catch (error) {
        return null
      }
    })()
    applyTheme(savedTheme || 'light')

    bindEvents()

    const bootstrap = await window.API.fetchBootstrap()
    agent = bootstrap.agent || agent
    suggestionList = Array.isArray(window.MOCK.suggestions) ? window.MOCK.suggestions.slice() : []

    const persisted = readJSON(STORAGE_KEY)
    if (persisted && persisted.sessions) {
      const sessions = normalizeSessions(persisted.sessions)
      if (sessions.length) {
        state.sessions = sessions
        state.currentId = sessions.some((item) => item.id === persisted.currentId)
          ? persisted.currentId
          : sessions[0].id
        state.panelCollapsed = persisted.panelCollapsed !== undefined
          ? !!persisted.panelCollapsed
          : window.innerWidth < 1024
        if (persisted.drafts && typeof persisted.drafts === 'object') {
          Object.keys(persisted.drafts).forEach((key) => {
            state.drafts[key] = String(persisted.drafts[key] || '')
          })
        }
        if (persisted.settings) Object.assign(state.settings, persisted.settings)
        if (persisted.card) {
          state.card.visible = persisted.card.visible !== false
          state.card.x = typeof persisted.card.x === 'number' ? persisted.card.x : null
          state.card.y = typeof persisted.card.y === 'number' ? persisted.card.y : null
        }
      }
    }

    if (!state.sessions.length) {
      state.sessions = normalizeSessions(bootstrap.sessions)
      state.currentId = state.sessions.length ? state.sessions[0].id : null
    }

    syncSettingsUI()
    setPanelCollapsed(state.panelCollapsed)
    setCardVisible(state.card.visible)

    renderAll()
    setStatus('idle')

    const rect = agentCard.getBoundingClientRect()
    const point =
      state.card.x !== null && state.card.y !== null
        ? placeCard(state.card.x, state.card.y)
        : placeCard(
            window.innerWidth - (agentCard.offsetWidth || rect.width || 196) - 26,
            window.innerHeight - (agentCard.offsetHeight || rect.height || 274) - 24
          )
    state.card.x = point.x
    state.card.y = point.y

    window.setTimeout(() => scrollToBottom(true), 60)

    if (ttsSupported) {
      window.speechSynthesis.getVoices()
    }
  }

  init().catch((error) => {
    console.error('[assistant prototype] init failed', error)
    toast('原型初始化失败，请刷新页面重试', 'error')
  })
})()
