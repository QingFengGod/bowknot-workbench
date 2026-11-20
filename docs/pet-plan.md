# 桌宠（Desktop Pet）实现计划

> 状态：P1–P4 已完成，素材转入序列帧片段库（2026-11-20）
> 关联文档：`docs/settings-plan.md`、`docs/artifact-plan.md`

## 1. 目标与非目标

首版要达成的效果：

- 桌宠作为独立透明窗口常驻桌面，始终置顶，不占任务栏。
- 角色以外区域鼠标完全穿透，不影响桌面正常操作。
- 可拖动、可吸附屏幕边缘、记住上次位置。
- 单击角色触发闲聊（走主助手短回复并语音播报）。
- 到点主动提醒，用动作 + 语音播报提示。
- 关闭主窗口时隐藏到托盘，桌宠与后台任务继续运行。

首版明确不做：

- Live2D / Spine 骨骼动画、视线跟随、多方向行走。
- 文字气泡（已确认改用语音播报）。
- 任务完成播报（首版只接闲聊、提醒、试听）。
- 用户自定义形象导入、开机自启、皮肤商店。

## 2. 已确认决策

| # | 决策 |
| --- | --- |
| 1 | 渲染走原生 Content：透明序列帧，不使用 WebView |
| 2 | 窗口为 `Frameless + Transparent + AlwaysOnTop + SkipTaskbar`，用 `SetInputRegion` 做形状级穿透 |
| 3 | 关闭主窗口 = 隐藏到托盘，桌宠与任务常驻；托盘菜单提供退出 |
| 4 | TTS 走豆包语音合成 2.0（`seed-tts-2.0` HTTP 接口），请求 PCM 后补 WAV 头播放 |
| 5 | 播报场景：单击闲聊、主动提醒、设置页试听；文本限长 60 字 |
| 6 | 形象素材内置（`go:embed` 打进二进制） |
| 7 | 提醒数据独立建 `reminder` 表（迁移版本 0012） |
| 8 | 拖动与点击分层判定：按下后位移小于 4 DIP 算点击，超过算拖动 |
| 9 | 素材按“一个动作一段序列帧”组织，不做骨骼与实时合成 |

## 3. 素材方案：透明序列帧片段库

每个动作是一段独立的透明序列帧，运行时只播一段，按事件优先级切换。不做参数化骨骼，也不在运行时合成两个动作；需要“说话 + 挥手”就单独生成一段。

### 3.1 片段清单

| 片段 | 时长 | 播放 | 触发 |
| --- | --- | --- | --- |
| `idle_a` / `idle_b` | 3–4 秒 | 循环 | 无操作，随机轮换 |
| `talk_a` / `talk_b` | 2–3 秒 | 循环 | 语音播报中 |
| `wave_a` | 1.5 秒 | 单次 | 打招呼、提醒 |
| `click` | 1.5 秒 | 单次 | 点击桌宠 |
| `sleep` | 3 秒 | 循环 | 长时间无操作 |

- 同一时刻只播一段；高优先级打断低优先级。
- 片段切换回到 idle 首帧，或做 2–3 帧交叉淡化。
- `sleep` 不单独生成时，用 idle 降帧加代码绘制的 Zzz 代替。

### 3.2 画布与锚点

| 项 | 规格 |
| --- | --- |
| 单帧尺寸 | 189×411 |
| 显示尺寸 | 138×300 |
| 锚点 | 脚底中心 (92, 410)，所有帧共用 |
| 帧率 | 8–12fps；手势类可用 12fps |
| 单段帧数 | 不超过 48 帧 |
| 图集 | 单张不超过 2048×2048，全部素材不超过 4 张 |
| 背景 | 透明 RGBA |

### 3.3 内存预算

- 一张 2048×2048 RGBA 图集约 16MB 显存，4 张合计 64MB 以内。
- idle 与 talk 图集常驻；click / wave / sleep 首次播放时加载，闲置 5 分钟后释放。
- 空闲时降到 4–8fps，睡眠 2fps；窗口小、GPU 绘制，CPU 占用可忽略。

### 3.4 pet.json 格式

```json
{
  "id": "bowknot",
  "version": 2,
  "display": { "width": 138, "height": 300 },
  "source": { "width": 189, "height": 411 },
  "anchor": { "x": 92, "y": 410 },
  "atlases": [
    { "file": "atlas-0.png", "width": 1512, "height": 1644 }
  ],
  "animations": {
    "idle_a": {
      "fps": 8,
      "loop": true,
      "frames": [
        { "atlas": 0, "x": 0, "y": 0, "w": 189, "h": 411, "duration": 125 }
      ]
    }
  },
  "hitRects": [
    { "x": 4, "y": 4, "w": 181, "h": 403 }
  ]
}
```

- `frames` 每项指向某张图集的一个矩形，`duration` 单位毫秒，缺省按 `fps` 推算。
- 校验：矩形不得越界、动作帧数不得为 0、`anchor` 必须在素材范围内；校验失败回退占位形象并告警。

### 3.5 目录与打包

```text
internal/pet/assets/bowknot/
├── atlas-0.png
├── atlas-1.png
└── pet.json
```

官方形象用 `go:embed` 打进二进制；后续用户自定义形象走数据目录 `~/.bowknot-workbench/pets/`。

## 4. 素材制作管线（即梦）

### 4.1 流程

1. 以 `resources/ref.png` 锁定角色外观。
2. 用参考图做图生视频，每个片段单独生成。
3. 源视频交给我：抽帧 → 洋红键控 → 去溢色 → 锚点对齐 → 打包图集 → 更新 `pet.json`。

### 4.2 生成参数

每段视频统一遵守：

- 竖版 3:4，720×960 或 1080×1440，24 或 30fps，单段 2–4 秒。
- **纯洋红背景 `#FF00FF`**，不要渐变、不要花纹、不要地面阴影。
- 固定机位，禁止推拉摇移、缩放、镜头切换。
- 角色全身入画，脚底位置全程不移动。
- 光照、色温、服装、发型与参考图一致。
- 不要字幕、水印、UI 元素、背景道具、人手。

提示词模板：

```text
全身角色，与参考图完全一致，固定机位，角色原地[动作描述]，
身体不位移，双脚位置不变，[表情要求]，
纯洋红色背景 #FF00FF，无阴影、无地面、无渐变、无字幕、无水印、无镜头运动，
竖版 3:4，[时长] 秒，24fps，[首尾要求]。
```

### 4.3 片段要求

| 片段 | 动作描述 | 首尾 |
| --- | --- | --- |
| `idle_a/b` | 原地呼吸起伏，自然眨眼 1–2 次，两段眨眼时机不同 | 首尾姿势一致，可循环 |
| `talk_a/b` | 只有嘴部张合与轻微点头 | 首尾闭嘴 |
| `wave_a` | 抬手招手，表情积极 | 结束接近 idle 首帧 |
| `click` | 闭眼微笑，身体轻微下沉再回弹 | 结束接近 idle 首帧 |
| `sleep` | 闭眼、身体缓慢起伏 | 首尾一致 |

### 4.4 交付与验收

- 目录：`resources/clips/<片段名>.mp4`。
- 先交 `talk_a` 样片，验证键控、边缘、对齐后再批量。
- 验收：角色外观一致；背景无残留、无洋红边；脚底不漂移；循环段首尾接近；无字幕水印。

## 5. 架构与目录

```text
internal/pet/                 纯逻辑，不依赖 UI，可单测
├── spec.go                   素材规格解析与校验（pet.json）
├── state.go                  片段与事件状态机
└── place.go                  吸附、边界、位置换算

internal/pet/assets/          官方形象，go:embed 打包
└── bowknot/                  图集 + pet.json

internal/ui/pet/              桌宠窗口壳
├── window.go                 窗口、input region、位置持久化
└── view.go                   取帧绘制

internal/tts/                 语音合成与播放
├── provider.go               Provider 接口
├── doubao.go                 豆包 seed-tts-2.0
└── speaker.go                串行播放、打断

internal/app/pet.go           业务装配：闲聊、试听、动作下发
internal/app/reminder.go      提醒调度与播报触发
internal/store/reminder.go    reminder 读写
```

依赖方向保持 `cmd → app → infra/store`，`pet` 只依赖标准库与领域模型。

装配位置在 [main.go](file:///d:/code_workspace/bowknot/bowknot-workbench/cmd/desktop/main.go)：创建 `tts`、`reminder`、`pet` 服务后注入 `shell.Deps`，桌宠窗口在 `shell.Start` 之后创建。

## 6. 窗口与输入

```go
mygo.NewWindow(mygo.WindowOptions{
    Frameless:        true,
    Transparent:      true,
    AlwaysOnTop:      true,
    SkipTaskbar:      true,
    DisableResize:    true,
    DisableShadow:    true,
    DisableMaximize:  true,
    Width:            178,
    Height:           332,
    Content:          ui.View(petView),
})
```

- 窗口 = 角色包围盒 + 头顶留白；不做尺寸伸缩。
- `SetInputRegion` 只覆盖角色可见区域，由 `hitRects` 按显示比例换算。
- 拖动与点击：按下记录起点，位移 ≤ 4 DIP 算点击；超过后交给系统窗口移动循环，松手吸附。
- 吸附：距工作区边缘小于 24 DIP 贴边，垂直位置保留。
- 位置持久化到 `PetSettings`（归一化坐标）；失效时回落主屏右下角。
- 托盘：左键唤起主窗口，右键菜单含显示/隐藏桌宠、打开主窗口、退出。

## 7. 状态机

```text
idle ──点击──> click ──结束──> idle
idle ──播报──> talk ──结束──> idle
idle ──无操作 5 分钟──> sleep ──点击/播报──> idle
任意 ──提醒触发──> wave + talk ──结束──> idle
```

优先级：提醒 > 拖动 > 点击 > 说话 > 睡眠 > 待机。

- 状态机放在 `internal/pet/state.go`，输入事件（Click、DragStart、DragEnd、SpeakStart、SpeakEnd、Remind、Tick），输出当前片段与帧序号，不接触 UI。
- 帧推进按片段 fps 计算，UI 侧用 ticker + `Invalidate` 驱动。
- 后台事件（提醒、TTS 结束）经壳层 `Update` 钩子回主线程再改状态。
- idle/talk 多段随机轮换，避免规律重复。

## 8. 语音播报

### 8.1 接口

```go
type Provider interface {
    Name() string
    Synthesize(ctx context.Context, req Request) (Result, error)
}
```

上层只认接口，切换服务商不改桌宠代码。

### 8.2 豆包实现

- HTTP 接口：`https://openspeech.bytedance.com/api/v3/plan/tts/unidirectional`；
- 请求头带 `X-Api-Key`、`X-Api-Resource-Id: seed-tts-2.0`、`X-Api-Request-Id`；
- 逐行解析响应中的 base64 音频分片，`code 20000000` 结束；
- 请求 PCM 24kHz，本地补 WAV 头交给 mygo `audio` 播放，不引入 mp3 解码依赖；
- 配置存在 `config.json` 的 `tts` 段，设置页「语音合成」可编辑，保存后下次点击生效。

### 8.3 播放与口型

- 串行播放，新播报打断旧播报。
- 文本截断到 60 字，超出以“等”结束。
- 真正开始播放时切到 `talk`，播完回 `idle`。
- 免打扰时段只做动作提醒，不发声；静音开关只静音不拦动作。

## 9. 提醒

### 9.1 表结构（迁移 0012_reminder.sql）

```sql
CREATE TABLE reminder (
  id            TEXT PRIMARY KEY,
  title         TEXT NOT NULL,
  speech        TEXT NOT NULL DEFAULT '',
  repeat_kind   TEXT NOT NULL DEFAULT 'once',
  next_fire_at  INTEGER NOT NULL,
  weekday       INTEGER NOT NULL DEFAULT 0,
  hour          INTEGER NOT NULL DEFAULT 0,
  minute        INTEGER NOT NULL DEFAULT 0,
  enabled       INTEGER NOT NULL DEFAULT 1,
  last_fired_at INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

CREATE INDEX idx_reminder_due ON reminder(enabled, next_fire_at);
```

### 9.2 调度规则

- 每分钟扫描 `enabled = 1 AND next_fire_at <= now`。
- 触发后写 `last_fired_at`，按 `repeat_kind` 推进 `next_fire_at`；`once` 触发后置 `enabled = 0`。
- 启动时若 `next_fire_at` 已过去超过 5 分钟，直接推进到下一次，不补播。
- 本地时区计算，存秒级时间戳；调度器放 `app/reminder.go`。

## 10. 设置项

`contract.Settings.PetSettings`：

```go
type PetSettings struct {
    Saved       bool    `json:"saved"`
    X           float64 `json:"x"`
    Y           float64 `json:"y"`
    Enabled     bool    `json:"enabled"`
    Scale       string  `json:"scale"`
    AlwaysOnTop bool    `json:"alwaysOnTop"`
    Voice       string  `json:"voice"`
    Volume      int     `json:"volume"`
    Muted       bool    `json:"muted"`
    QuietStart  string  `json:"quietStart"`
    QuietEnd    string  `json:"quietEnd"`
}
```

- 已实现：位置保存与恢复。
- 待实现：启用开关、缩放、置顶、音色、音量、静音、免打扰、试听。
- 保存即生效；缩放与置顶立即应用到窗口。

## 11. 生命周期

- 主窗口关闭改为隐藏，首次隐藏时托盘气泡提示一次。
- 托盘常驻，退出入口在托盘菜单；退出时依次关闭提醒调度、TTS 队列、桌宠窗口。
- 启动时若启用则创建桌宠窗口，否则只保留托盘。
- 桌宠窗口销毁走 `Close()`，`OnQuit` 中统一清理。

## 12. 实施阶段与进度

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| P1 | `pet.json` 解析与校验、图集加载 | 完成 |
| P2 | 透明窗口、input region、拖动吸附、位置持久化 | 完成 |
| P3 | 点击/拖动判定、托盘、主窗口关闭隐藏 | 完成 |
| P4 | 豆包 TTS、设置页配置、点击播报 | 完成 |
| P5 | reminder 表、调度器、提醒播报 | 待开始 |
| P6 | 片段库生成、状态机扩展、替换临时素材 | 进行中 |

当前素材：`idle` 为正式图集；`talk` / `click` 为临时验证素材，片段库到位后替换。

## 13. 风险

| 风险 | 应对 |
| --- | --- |
| 洋红键控在发丝、裙边留下残色 | 先出 `talk_a` 样片验证；边缘收缩 1px + 去溢色 |
| 片段首尾不一致导致切换跳变 | 回到 idle 首帧再切，或 2–3 帧交叉 |
| 片段数量增长导致图集超预算 | 单段 ≤48 帧；非 idle/talk 按需加载并释放 |
| 角色在各段视频里外观漂移 | 全部以同一参考图生成；交付前逐段比对 |
| 多屏与缩放变化 | 位置存归一化坐标，失效时回落主屏右下角 |
| 无托盘环境 | 托盘创建失败时降级为普通窗口，不阻塞启动 |

## 14. 人工验收清单

- [ ] 桌宠常驻桌面，其他应用可正常点击其身后的区域。
- [ ] 拖动顺畅，贴边吸附，重启后位置保持。
- [ ] 单击有动作反馈并播放语音，长文本会截断。
- [ ] 片段切换无跳变，背景无残留、无彩色边。
- [ ] 播报时循环 talk，结束回 idle；idle 不规律重复。
- [ ] 提醒到点触发动作与播报，免打扰时段只动不响。
- [ ] 关闭主窗口后托盘可见，可从托盘唤起与退出。
- [ ] 退出应用后进程完全结束。
