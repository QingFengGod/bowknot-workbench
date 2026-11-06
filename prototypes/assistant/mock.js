/* mock.js — 单一数据源：主 Agent、会话、消息与演示文案 */
;(function () {
  const agent = {
    id: 'main-agent',
    name: '小蝶',
    tagline: '主 Agent · 日常陪伴',
    status: '在线',
    model: '小蝶 · 标准',
    voice: '温柔女声',
    greeting: '欢迎回来。今天想先聊点什么？'
  }

  const suggestions = [
    '帮我理一下今天的安排',
    '陪我聊会儿天',
    '记住我周末的习惯'
  ]

  const messages = {
    daily: [
      {
        id: 'm-d-1',
        role: 'agent',
        content:
          '早上好呀。我把窗边的光调暖了一点，今天适合慢慢说。\n\n昨晚睡得还好吗？',
        time: '09:02',
        status: 'done',
        emotion: 'happy'
      },
      {
        id: 'm-d-2',
        role: 'user',
        content: '还行，就是事情有点多。帮我把今天要做的事理一理。',
        time: '09:04',
        status: 'done'
      },
      {
        id: 'm-d-3',
        role: 'agent',
        content:
          '好，我按轻重缓急排了一下：\n\n1. **10:30 项目周会** — 提前整理上周进展\n2. **14:00 设计评审** — 重点看助手页的会话面板\n3. **晚上** — 留一点时间读书和散步\n\n第一项要不要拆成三个更小的动作？',
        time: '09:05',
        status: 'done',
        emotion: 'think',
        action: 'nod'
      },
      {
        id: 'm-d-4',
        role: 'user',
        content: '嗯。顺便记一下，我周末喜欢上午整理房间。',
        time: '09:07',
        status: 'done'
      },
      {
        id: 'm-d-5',
        role: 'agent',
        content:
          '记下了。周末上午留给整理和打扫，下午再安排出门，这样周日晚上会轻松很多。\n\n现在先看周会材料吧，我陪你。',
        time: '09:07',
        status: 'done',
        emotion: 'shy',
        memoryWritten: true
      },
      {
        id: 'm-d-6',
        role: 'user',
        content: '好，那我现在先做什么？',
        time: '09:09',
        status: 'done'
      },
      {
        id: 'm-d-7',
        role: 'agent',
        content:
          '先花 10 分钟列出“上周完成 / 本周重点 / 需要协调”三行。不用写完整，先把会议要说的骨架搭出来。',
        time: '09:09',
        status: 'done',
        emotion: 'neutral',
        action: 'wave'
      }
    ],
    weekend: [
      {
        id: 'm-w-1',
        role: 'agent',
        content: '周六上午整理房间，下午想出门吗？天气看起来不错。',
        time: '07:40',
        status: 'done',
        emotion: 'happy'
      },
      {
        id: 'm-w-2',
        role: 'user',
        content: '想去看那个摄影展，晚上再去吃面。',
        time: '07:42',
        status: 'done'
      },
      {
        id: 'm-w-3',
        role: 'agent',
        content:
          '那就把周六下午留给展览，晚上去那家面馆。\n\n我帮你留了 2 小时看展，路上各 30 分钟，不会太赶。',
        time: '07:43',
        status: 'done',
        emotion: 'happy',
        action: 'nod'
      },
      {
        id: 'm-w-4',
        role: 'user',
        content: '完美，帮我记到周末计划里。',
        time: '07:44',
        status: 'done'
      },
      {
        id: 'm-w-5',
        role: 'agent',
        content: '已经放进「周末计划」。需要我周五晚上提醒你一次吗？',
        time: '07:44',
        status: 'done',
        emotion: 'neutral',
        memoryWritten: true
      }
    ],
    writing: [
      {
        id: 'm-wr-1',
        role: 'user',
        content: '我想写一个关于雨夜重逢的短篇，但开头总是很平。',
        time: '昨天 21:10',
        status: 'done'
      },
      {
        id: 'm-wr-2',
        role: 'agent',
        content:
          '这个开头可以从雨声切入，不要先交代人物：\n\n> 雨落在旧车站的铁皮顶上，像有人在很远的地方，一颗一颗地拨着算盘。\n\n先让读者听见，再让他们看见。',
        time: '昨天 21:11',
        status: 'done',
        emotion: 'think'
      },
      {
        id: 'm-wr-3',
        role: 'user',
        content: '这个意象我喜欢。下一句怎么接？',
        time: '昨天 21:13',
        status: 'done'
      },
      {
        id: 'm-wr-4',
        role: 'agent',
        content:
          '下一句别急着让人物出场，先写她手里的伞——伞骨断了一根，雨水顺着缺口落在她手背上。动作比说明更有分量。',
        time: '昨天 21:13',
        status: 'done',
        emotion: 'happy',
        action: 'wave'
      }
    ],
    review: [
      {
        id: 'm-r-1',
        role: 'user',
        content: '这次项目延期，问题到底出在哪？',
        time: '周一 18:20',
        status: 'done'
      },
      {
        id: 'm-r-2',
        role: 'agent',
        content:
          '问题不在排期，而在验收标准没有提前对齐。设计、开发、测试对“完成”的理解不一致，返工自然发生。\n\n下次评审时，把验收样例一起定下来。',
        time: '周一 18:21',
        status: 'done',
        emotion: 'neutral',
        action: 'nod'
      }
    ],
    travel: [
      {
        id: 'm-t-1',
        role: 'user',
        content: '下周出差三天，帮我列个行李清单。',
        time: '上周 14:05',
        status: 'done'
      },
      {
        id: 'm-t-2',
        role: 'agent',
        content:
          '证件、充电线、常用药，先放进行李箱侧袋。\n\n衣物按“两天一换 + 一件外套”准备，会议材料放电脑包外层，过安检时不用翻。',
        time: '上周 14:06',
        status: 'done',
        emotion: 'neutral',
        memoryWritten: true
      }
    ],
    emotion: [
      {
        id: 'm-e-1',
        role: 'user',
        content: '今天什么都不想做，感觉自己很没用。',
        time: '上周 22:40',
        status: 'done'
      },
      {
        id: 'm-e-2',
        role: 'agent',
        content:
          '你已经很努力了，今天先允许自己慢一点。\n\n不用把“休息”也变成一项要完成的任务。要不要先从喝一杯水开始？',
        time: '上周 22:41',
        status: 'done',
        emotion: 'sad'
      }
    ],
    fitness: [
      {
        id: 'm-f-1',
        role: 'user',
        content: '想开始健身，但总坚持不下来。',
        time: '10月28日 19:30',
        status: 'done'
      },
      {
        id: 'm-f-2',
        role: 'agent',
        content: '先从每周两次、每次二十分钟开始。目标小到不会失败，才有下一次。',
        time: '10月28日 19:31',
        status: 'done',
        emotion: 'happy',
        action: 'nod'
      }
    ],
    reading: [
      {
        id: 'm-b-1',
        role: 'user',
        content: '《注意力》第三章有点难懂。',
        time: '10月24日 21:00',
        status: 'done'
      },
      {
        id: 'm-b-2',
        role: 'agent',
        content: '把「注意力是有限的」记在第三章旁边。后面所有方法，都是围绕如何分配这份有限资源。',
        time: '10月24日 21:01',
        status: 'done',
        emotion: 'neutral'
      }
    ]
  }

  const sessions = [
    {
      id: 'daily',
      title: '日常',
      preview: '先花 10 分钟列出会议要说的骨架。',
      updatedAt: '刚刚',
      updatedOrder: 10,
      messages: messages.daily
    },
    {
      id: 'weekend',
      title: '周末计划',
      preview: '已经放进「周末计划」，周五晚上提醒你。',
      updatedAt: '2 小时前',
      updatedOrder: 9,
      messages: messages.weekend
    },
    {
      id: 'writing',
      title: '写作灵感',
      preview: '动作比说明更有分量。',
      updatedAt: '昨天',
      updatedOrder: 8,
      messages: messages.writing
    },
    {
      id: 'review',
      title: '项目复盘',
      preview: '验收标准要提前对齐。',
      updatedAt: '周一',
      updatedOrder: 7,
      messages: messages.review
    },
    {
      id: 'travel',
      title: '旅行清单',
      preview: '证件、充电线、常用药放侧袋。',
      updatedAt: '上周',
      updatedOrder: 6,
      messages: messages.travel
    },
    {
      id: 'emotion',
      title: '情绪树洞',
      preview: '今天先允许自己慢一点。',
      updatedAt: '上周',
      updatedOrder: 5,
      messages: messages.emotion
    },
    {
      id: 'fitness',
      title: '健身安排',
      preview: '每周两次，每次二十分钟。',
      updatedAt: '10月28日',
      updatedOrder: 4,
      messages: messages.fitness
    },
    {
      id: 'reading',
      title: '读书笔记',
      preview: '注意力是有限的。',
      updatedAt: '10月24日',
      updatedOrder: 3,
      messages: messages.reading
    }
  ]

  const replyBank = [
    {
      keywords: ['安排', '计划', '理一理', '今天'],
      content:
        '好，我先把今天分成三段：\n\n1. **上午** — 把周会要说的三行骨架写出来\n2. **下午** — 评审前留 20 分钟过一遍会话面板\n3. **晚上** — 只保留读书和散步，不再加任务\n\n要不要我按这个节奏，在下午评审前提醒你一次？',
      emotion: 'think',
      action: 'nod',
      memory: false
    },
    {
      keywords: ['累', '难过', '没用', '不想'],
      content:
        '听起来你今天已经撑了很久。\n\n我们先不解决任何问题，只做一件小事：把肩膀放松，喝口水。等你想说的时候，我在。',
      emotion: 'sad',
      action: 'idle',
      memory: false
    },
    {
      keywords: ['记住', '记一下', '记到', '习惯'],
      content:
        '记下了。\n\n我会把这条放进长期记忆，之后在相关对话里主动想起它，不需要你反复提醒。',
      emotion: 'shy',
      action: 'nod',
      memory: true
    },
    {
      keywords: ['写', '开头', '灵感', '故事'],
      content:
        '可以先把“解释”拿掉，只留一个可感知的动作。\n\n比如：她没有撑伞，只把外套领口往上拉了拉。读者会自己追问原因，这比直接说明更有牵引力。',
      emotion: 'think',
      action: 'idle',
      memory: false
    },
    {
      keywords: ['你好', '在吗', '陪我', '聊'],
      content:
        '在的。\n\n今天不用表现得很好，想到什么就说什么。哪怕只是几句零碎的话，我也会认真接住。',
      emotion: 'happy',
      action: 'wave',
      memory: false
    }
  ]

  const fallbackReply = {
    content:
      '我听到了。\n\n这件事可以拆成“现在能做的”和“需要等的”两部分。你愿意先说说最卡住你的那一小步吗？',
    emotion: 'neutral',
    action: 'nod',
    memory: false
  }

  const transcriptions = [
    '帮我把今天下午的安排再理一遍。',
    '我有点累，陪我安静地聊一会儿。',
    '记住我周末上午喜欢整理房间。',
    '帮我写一个雨夜重逢的开头。'
  ]

  window.MOCK = {
    agent,
    suggestions,
    sessions,
    replyBank,
    fallbackReply,
    transcriptions
  }
})()
