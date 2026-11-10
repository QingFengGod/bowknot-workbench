import type { StreamReply } from '@/composables/useMessageStream'
import type { ChatMessage, MessagePart } from '@/types/chat'
import type { ConversationItem } from './type'

const now = Date.now()
const MINUTE = 60 * 1000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

const movieSessions: ConversationItem[] = Array.from({ length: 10 }, (_, index) => ({
  id: `movie${index + 1}`,
  title: '片单',
  summary: '把想看的十部电影按情绪分成了三类。',
  updatedAt: now - 12 * DAY
}))

export const mockConversations: ConversationItem[] = [
  {
    id: 'daily',
    group: '日常',
    title: '日常',
    summary: '下周计划已经整理好，按天推进即可。',
    updatedAt: now - 2 * MINUTE
  },
  {
    id: 'work',
    group: '工作',
    title: '工作讨论',
    summary: '周报要点分三块：本周进展、风险与下周计划。',
    updatedAt: now - 36 * MINUTE
  },
  {
    id: 'travel',
    group: '日常',
    title: '旅行计划',
    summary: '杭州三天两晚，按西湖、灵隐、龙井的顺序排。',
    updatedAt: now - 5 * HOUR
  },
  {
    id: 'reading',
    group: '工作',
    title: '读书笔记',
    summary: '把《禅与摩托车维修艺术》的摘录按章节归档。',
    updatedAt: now - (DAY + 3 * HOUR)
  },
  {
    id: 'fitness',
    group: '身体健康',
    title: '健身打卡',
    summary: '本周完成 3 次力量训练，下一次建议练背。',
    updatedAt: now - 3 * DAY
  },
  ...movieSessions
]

type MessageExtra = {
  error?: string
  memoryWritten?: boolean
}

const createMessage = (
  id: string,
  conversationId: string,
  role: ChatMessage['role'],
  parts: MessagePart[],
  status: ChatMessage['status'],
  minutesAgo: number,
  extra: MessageExtra = {}
): ChatMessage => ({
  id,
  conversationId,
  role,
  parts,
  status,
  createdAt: now - minutesAgo * MINUTE,
  ...extra
})

const textPart = (text: string): MessagePart => ({ type: 'text', text })

const shortAnswer = `本周进展可以拆成三块：

1. **消息渲染**：完成块级切分与缓存，流式过程中只重算最后一个未完成的块。
2. **交互闭环**：补齐停止、重试、复制与删除，失败消息不再需要刷新页面。
3. **待观察项**：长文本下的滚动跟随还有轻微抖动，下周优先处理。

下周建议先做代码高亮的异步加载，再补工具调用卡片的样式。`

const travelStopped = `杭州三天两晚可以这样安排：

第一天中午到酒店放行李，下午沿西湖走北线，从断桥到孤山，傍晚在湖滨看日落。

第二天上午去灵隐寺，下午转龙井村，找一家茶室坐一会儿。

第三天留给河坊街和南宋御街，下午返程。`

const travelAnswer = `${travelStopped}

补充几点：

- 西湖边周末人多，北线尽量放在工作日或清晨。
- 灵隐寺需要提前预约，飞来峰门票单独购买。
- 龙井村到九溪有一段徒步路线，穿轻便的鞋。
- 返程当天留出至少两小时到高铁站，避免晚高峰。`

const readingError = `我先按章节把摘录重新排了一遍，第一部分已经整理好，后面两部分还需要确认几个页码。`

const longAnswer = `## 下周工作计划

先给结论：下周的重点是**把工作台的消息渲染链路打通**，同时保证会话管理不出现回归。下面的安排按目标、节奏、实现、风险四部分展开，你可以按天取用，也可以整体顺延。

### 一、目标拆解

1. **消息模型统一**：用户消息与助手消息共用一套 \`parts\` 结构，文本、代码、工具调用都作为独立 part 存在，渲染层不再判断“这段话属于谁”。
2. **流式渲染稳定**：生成过程中不整段重排，只更新最后一个未完成的块，已完成块直接复用缓存结果。
3. **交互闭环**：停止、重试、复制、重答、删除都能在消息区完成，不依赖额外的弹窗或页面跳转。
4. **可观测性**：开发环境打印单帧渲染耗时，超过阈值时给出提示，避免性能回退被悄悄合入。

这四个目标里，前两个是地基，后两个是体验。地基不稳时先不要急着做动画，否则每加一种消息类型都要回头改渲染链路，返工成本会成倍增加。

### 二、每日节奏

| 日期 | 上午 | 下午 |
| --- | --- | --- |
| 周一 | 数据模型与 mock 数据 | Markdown 切块与缓存 |
| 周二 | 流式驱动器 | 停止与错误分支 |
| 周三 | 消息容器与操作栏 | 自动滚动与回到底部 |
| 周四 | 代码块与非文本 part | 明暗主题适配 |
| 周五 | 联调与回归 | 文档与验收记录 |

表格里的安排留了缓冲。遇到阻塞时优先保证主链路，样式细节可以顺延到下一周，但不要跳过错误分支的验证，因为错误分支往往在演示时最容易暴露。

### 三、关键实现

先把切块逻辑收敛成纯函数，输入是完整文本，输出是带 \`done\` 标记的块数组。这样组件层不需要理解 Markdown 语法，只负责渲染和节流。

~~~ts
type Block = { id: string; content: string; done: boolean }

export function splitBlocks(text: string): Block[] {
  const blocks: string[] = []
  let buffer: string[] = []

  for (const line of text.split('\\n')) {
    if (!line.trim()) {
      if (buffer.length) blocks.push(buffer.join('\\n'))
      buffer = []
      continue
    }
    buffer.push(line)
  }
  if (buffer.length) blocks.push(buffer.join('\\n'))

  return blocks.map((content, index) => ({
    id: \`block-\${index}\`,
    content,
    done: index < blocks.length - 1
  }))
}
~~~

渲染层只对未完成的块做节流，已完成块直接复用缓存的 HTML 字符串。缓存键使用块内容本身，避免维护额外的版本号；缓存达到上限时整体清空，简单但足够应对单次会话。

~~~ts
const cache = new Map<string, string>()

export function renderBlock(block: Block): string {
  if (block.done) {
    const cached = cache.get(block.content)
    if (cached) return cached
  }
  const html = sanitize(markdown.render(block.content))
  if (block.done) cache.set(block.content, html)
  return html
}
~~~

自动滚动只在用户位于底部时生效。用户上滚后立即让出控制权，并在右下角显示“回到底部”，生成结束后也不自动抢回位置。

~~~ts
const distance = el.scrollHeight - el.scrollTop - el.clientHeight

if (distance < 80) {
  el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
}
~~~

### 四、风险与应对

- **语法未闭合**：围栏代码在流式中可能只出现一半，渲染前补齐结尾，避免解析器吞掉后续内容。
- **表格抖动**：未完成的表格先按普通文本展示，等分隔行出现后再切换成表格，避免列宽反复跳动。
- **滚动争抢**：生成时如果用户主动上滚，暂停吸底；用户回到底部后再恢复跟随。
- **性能回退**：为每个已完成块建立缓存，长文本只重算最后一块，单帧耗时控制在 2ms 以内。
- **主题兼容**：所有颜色走 token，不在组件里写死色值；动画补充 reduced-motion 降级。

### 五、验收清单

1. 长文本流式输出过程中没有明显掉帧，滚动条不抖动。
2. 停止生成后保留已生成内容，重试能重新开始。
3. 失败消息展示错误原因，不覆盖用户已输入的内容。
4. 复制得到纯文本，代码块复制保留语言标记。
5. 明暗主题下消息、代码块、操作栏都清晰可读。
6. 未注册的 part 类型有兜底占位，不抛出运行时错误。

### 六、下周复盘问题

- 消息数量增长到 50 条以上时，是否需要引入虚拟滚动？
- 代码高亮放在流式结束后异步执行，是否会造成明显的颜色跳变？
- 工具调用与卡片消息的注册表接口是否足够稳定，能否避免改分发组件？
- 会话切换时继续生成，是否需要给用户更明确的提示？
- 多会话同时生成时，滚动跟随应该归属哪个会话？

### 七、给你的建议

如果时间有限，优先完成前三项：消息模型、块级渲染、停止与重试。这三项决定了后续功能的地基。操作栏、播报、富卡片都可以在下一周补齐。反过来，如果顺序颠倒，先做样式和动画，后面每加一种 part 都要改一次渲染链路，返工成本会更高。

另外，建议每天结束前花十分钟记录当天的阻塞点和临时决策。很多问题在第二天回看时会变得很清楚，也能避免同样的坑踩第二次。下周复盘时，把这份记录和验收清单放在一起看，基本就能判断这一周是“完成”还是“看起来完成”。

最后提醒一句：计划是给人用的，不是用来证明自己排得很满。如果某一天状态不好，把当天最重要的一件事做完就够了，剩下的顺延不会影响整体节奏。

还有一点容易被忽略：给写代码的人留出整块时间。消息渲染这类工作最怕被切碎，刚进入状态就被叫去开会，回来又要重新梳理上下文。建议把沟通集中到下午的固定时段，上午只保留一件需要深度思考的任务。真遇到临时需求，也先记下来，等手上这块告一段落再处理，效率反而更高。

如果连续两天都没能完成当天的主线任务，就把计划砍掉三分之一，先保证节奏，再谈完整度。`

export const mockMessages: Record<string, ChatMessage[]> = {
  daily: [
    createMessage('daily-u1', 'daily', 'user', [textPart('帮我整理一份下周的工作计划，越详细越好。')], 'done', 3)
  ],
  work: [
    createMessage('work-u1', 'work', 'user', [textPart('帮我把这周的进展整理成周报要点。')], 'done', 40),
    createMessage(
      'work-a1',
      'work',
      'assistant',
      [{ type: 'emotion', emotion: 'happy' }, textPart(shortAnswer)],
      'done',
      39,
      { memoryWritten: true }
    )
  ],
  travel: [
    createMessage(
      'travel-u1',
      'travel',
      'user',
      [textPart('杭州三天怎么玩？帮我排个不赶的行程。')],
      'done',
      5 * 60 + 20
    ),
    createMessage('travel-a1', 'travel', 'assistant', [textPart(travelStopped)], 'stopped', 5 * 60 + 19)
  ],
  reading: [
    createMessage(
      'reading-u1',
      'reading',
      'user',
      [textPart('把上次的读书笔记按章节重新整理一下。')],
      'done',
      DAY + 4 * HOUR
    ),
    createMessage('reading-a1', 'reading', 'assistant', [textPart(readingError)], 'failed', DAY + 4 * HOUR - 1, {
      error: '模型服务暂时不可用，请稍后重试'
    })
  ]
}

export const mockReplies: Record<string, StreamReply> = {
  daily: { text: longAnswer },
  work: { text: shortAnswer },
  travel: { text: travelAnswer },
  reading: {
    text: `${readingError} 重新整理后的版本如下：第一章讲旅行与维修的关系，第二章开始讨论“良质”的定义。`,
    errorAt: 60,
    errorMessage: '网络连接中断，请稍后重试'
  }
}
