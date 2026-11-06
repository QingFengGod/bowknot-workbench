/* api.js — 异步服务桩层。签名按未来真实 API 设计，内部只返回 mock 数据。
   接入后端时逐个替换实现，UI 与状态层不需要重写。 */
;(function () {
  const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

  function pickReply(text) {
    const source = String(text || '').toLowerCase()
    const hit = window.MOCK.replyBank.find((item) =>
      item.keywords.some((keyword) => source.includes(keyword.toLowerCase()))
    )
    return hit || window.MOCK.fallbackReply
  }

  const api = {
    /**
     * TODO: GET /api/assistant/sessions
     * response: { agent, sessions }
     */
    async fetchBootstrap() {
      await delay(280)
      return {
        agent: { ...window.MOCK.agent },
        sessions: JSON.parse(JSON.stringify(window.MOCK.sessions))
      }
    },

    /**
     * TODO: POST /api/assistant/sessions
     * request: { title }
     * response: { id, title, preview, updatedAt, messages }
     */
    async createSession({ title }) {
      await delay(160)
      return {
        id: 'session-' + Date.now(),
        title,
        preview: '还没有消息',
        updatedAt: '刚刚',
        messages: []
      }
    },

    /**
     * TODO: PATCH /api/assistant/sessions/:id
     * request: { title }
     * response: { ok: true }
     */
    async renameSession({ id, title }) {
      await delay(120)
      if (!title.trim()) throw new Error('会话标题不能为空')
      return { id, title: title.trim(), ok: true }
    },

    /**
     * TODO: DELETE /api/assistant/sessions/:id
     * response: { ok: true }
     */
    async deleteSession({ id }) {
      await delay(140)
      return { id, ok: true }
    },

    /**
     * TODO: POST /api/assistant/chat  (SSE)
     * request: { sessionId, message }
     * stream: { type: 'delta', text } / { type: 'done', emotion, action, memory }
     */
    async sendMessage({ text, onChunk, signal }) {
      await delay(620)
      const reply = pickReply(text)
      const chars = Array.from(reply.content)
      let buffer = ''

      for (let index = 0; index < chars.length; index += 1) {
        if (signal && signal.aborted) {
          return { aborted: true, content: buffer, emotion: 'neutral', action: 'idle', memory: false }
        }
        buffer += chars[index]
        if (onChunk) onChunk(buffer, reply)
        // 标点处稍作停顿，模拟真实流式节奏
        const pause = /[，。！？\n]/.test(chars[index]) ? 90 : 18
        await delay(pause)
      }

      return {
        aborted: false,
        content: reply.content,
        emotion: reply.emotion,
        action: reply.action,
        memory: reply.memory
      }
    },

    /**
     * TODO: POST /api/assistant/voice/transcribe
     * request: audio blob
     * response: { text, confidence }
     */
    async transcribe({ duration }) {
      await delay(760)
      if (duration < 0.45) return { text: '', confidence: 0, error: '说话时间太短' }
      const list = window.MOCK.transcriptions
      const text = list[Math.floor(Math.random() * list.length)]
      return { text, confidence: 0.94 }
    },

    /**
     * TODO: POST /api/assistant/voice/synthesize
     * request: { text, voice }
     * response: { audioUrl }（当前原型使用浏览器 speechSynthesis 代替）
     */
    async synthesize({ text }) {
      return {
        supported: typeof window.speechSynthesis !== 'undefined' && typeof window.SpeechSynthesisUtterance !== 'undefined',
        text
      }
    }
  }

  window.API = api
})()
