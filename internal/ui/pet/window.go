// Package pet 是桌宠的独立透明窗口：置顶、无边框、角色以外区域鼠标穿透。
package pet

import (
	"fmt"
	"sync"
	"time"

	petasset "bowknot-workbench/internal/pet"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// marginX 是角色左右留白，marginTop / marginBottom 是上下留白。
	marginX      = 20
	marginTop    = 16
	marginBottom = 16
	// screenMargin 是首次出现时距工作区右下角的距离，需大于吸附阈值。
	screenMargin = 40
	// snapDistance 是松手后吸附到屏幕边缘的距离阈值。
	snapDistance = 24
	// dragThreshold 是判定为拖动而非点击的位移（DIP）。
	dragThreshold = 4
)

// Position 是窗口左上角相对工作区的归一化坐标，取值 0–1。
type Position struct {
	X float64
	Y float64
}

// Options 是创建桌宠窗口的参数。
type Options struct {
	// Bundle 是加载完成的形象素材，必须包含 idle 动作。
	Bundle *petasset.Bundle
	// Animation 是默认播放的动作名，空为 idle。
	Animation string
	// Position 是上次保存的位置，nil 表示首次启动、落到右下角。
	Position *Position
	// SavePosition 在拖动结束后回调归一化位置。
	SavePosition func(x, y float64)
	// OnClick 在判定为点击时回调，拖动不会触发。
	OnClick func()
}

// Pet 是桌宠窗口及其播放状态。窗口内容按时间取帧，不持有逐帧状态。
type Pet struct {
	win     *mygo.Window
	bitmaps []*ui.Bitmap
	spec    petasset.Spec
	anim    petasset.Animation
	// animName 是当前动作名，非循环动作播完回到 idle。
	animName string

	width, height int
	charX, charY  float32
	hitRect       mygo.Rectangle

	started time.Time
	stop    chan struct{}
	once    sync.Once

	onClick      func()
	savePosition func(x, y float64)
	// pressActive 表示鼠标按住角色；dragMoved 表示位移已超过点击阈值。
	pressActive bool
	dragMoved   bool
	// suppressUntil 屏蔽拖动结束后残留的按下状态，避免误判成一次点击。
	suppressUntil time.Time
	pressCursor   mygo.Point
	pressX        int
	pressY        int
}

// New 解码素材并创建桌宠窗口，落在主屏工作区右下角。
func New(opts Options) (*Pet, error) {
	if opts.Bundle == nil {
		return nil, fmt.Errorf("缺少桌宠素材")
	}
	name := opts.Animation
	if name == "" {
		name = "idle"
	}
	spec := opts.Bundle.Spec
	anim, ok := spec.Animations[name]
	if !ok {
		return nil, fmt.Errorf("形象缺少动作 %s", name)
	}

	p := &Pet{
		spec:         spec,
		anim:         anim,
		animName:     name,
		width:        spec.Display.Width + marginX*2,
		height:       spec.Display.Height + marginTop + marginBottom,
		charX:        marginX,
		charY:        marginTop,
		started:      time.Now(),
		stop:         make(chan struct{}),
		onClick:      opts.OnClick,
		savePosition: opts.SavePosition,
	}

	p.bitmaps = make([]*ui.Bitmap, len(spec.Atlases))
	for i, atlas := range spec.Atlases {
		raw, ok := opts.Bundle.Atlases[atlas.File]
		if !ok {
			return nil, fmt.Errorf("缺少图集 %s", atlas.File)
		}
		bitmap, err := ui.DecodeBitmap(raw)
		if err != nil {
			return nil, fmt.Errorf("解码图集 %s 失败: %w", atlas.File, err)
		}
		p.bitmaps[i] = bitmap
	}

	p.hitRect = p.characterRect()

	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title:             "Bowknot Pet",
		Frameless:         true,
		Transparent:       true,
		AlwaysOnTop:       true,
		SkipTaskbar:       true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		DisableShadow:     true,
		Width:             p.width,
		Height:            p.height,
		Content:           ui.View(p.view),
	})

	// 只有角色包围盒接收鼠标，其余区域直接穿透到桌面。
	p.win.SetInputRegion([]mygo.Rectangle{p.hitRect})
	if opts.Position != nil {
		p.restore(*opts.Position)
	} else {
		p.moveToCorner()
	}
	go p.play()

	return p, nil
}

// Window 返回底层窗口，供壳层统一管理显示与隐藏。
func (p *Pet) Window() *mygo.Window { return p.win }

// Play 切换动作并从头播放，未知动作保持当前状态。
// 只允许在主线程调用（点击回调、窗口 Update 内部）。
func (p *Pet) Play(name string) {
	if name == p.animName {
		return
	}
	anim, ok := p.spec.Animations[name]
	if !ok {
		return
	}
	p.animName = name
	p.anim = anim
	p.started = time.Now()
	if p.win != nil && !p.win.IsDestroyed() {
		p.win.Invalidate()
	}
}

// Stop 停止帧调度，可重复调用。
func (p *Pet) Stop() {
	p.once.Do(func() {
		close(p.stop)
	})
}

// characterRect 把素材命中矩形换算成窗口内的 DIP 坐标。
func (p *Pet) characterRect() mygo.Rectangle {
	hit := p.spec.HitRects
	if len(hit) == 0 {
		return mygo.Rectangle{
			X:      marginX,
			Y:      marginTop,
			Width:  p.spec.Display.Width,
			Height: p.spec.Display.Height,
		}
	}
	scaleX := float64(p.spec.Display.Width) / float64(p.spec.Source.Width)
	scaleY := float64(p.spec.Display.Height) / float64(p.spec.Source.Height)
	r := hit[0]
	return mygo.Rectangle{
		X:      marginX + int(float64(r.X)*scaleX),
		Y:      marginTop + int(float64(r.Y)*scaleY),
		Width:  int(float64(r.Width) * scaleX),
		Height: int(float64(r.Height) * scaleY),
	}
}

// moveToCorner 把窗口放到主屏工作区右下角。
func (p *Pet) moveToCorner() {
	area := mygo.Screen.PrimaryDisplay().WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		return
	}
	p.win.SetPosition(
		area.X+area.Width-p.width-screenMargin,
		area.Y+area.Height-p.height-screenMargin,
	)
}

// restore 按保存的归一化坐标放置窗口，并保证完整落在工作区内。
func (p *Pet) restore(pos Position) {
	area := mygo.Screen.PrimaryDisplay().WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		return
	}
	x := area.X + int(pos.X*float64(area.Width))
	y := area.Y + int(pos.Y*float64(area.Height))
	x = min(max(x, area.X), area.X+area.Width-p.width)
	y = min(max(y, area.Y), area.Y+area.Height-p.height)
	p.win.SetPosition(x, y)
}

// play 按动作帧率请求重绘；取帧本身按时间计算，丢帧不会拖慢动作。
func (p *Pet) play() {
	interval := time.Second / 8
	if p.anim.FPS > 0 {
		interval = time.Duration(float64(time.Second) / p.anim.FPS)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			if p.win.IsDestroyed() {
				return
			}
			p.win.Invalidate()
		}
	}
}

// view 画出当前帧；整个窗口只有角色区域接收输入。
func (p *Pet) view(c *ui.Context) {
	// 根节点默认绘制主题背景，透明窗口必须显式清空，否则会出现整块底色。
	c.Root().Background(ui.Transparent)
	elapsed := time.Since(p.started)
	// 非循环动作播完自动回到 idle。
	if !p.anim.Loop && elapsed >= p.anim.Duration() {
		p.Play("idle")
		elapsed = 0
	}
	box := ui.Box(c).Fill().Draw(func(painter *ui.Painter, _ ui.Rect) {
		frame := p.anim.Frames[p.anim.FrameAt(elapsed)]
		painter.ImageRegion(p.bitmaps[frame.Atlas],
			ui.Rect{X: float32(frame.X), Y: float32(frame.Y), W: float32(frame.Width), H: float32(frame.Height)},
			ui.Rect{X: p.charX, Y: p.charY, W: float32(p.spec.Display.Width), H: float32(p.spec.Display.Height)},
			ui.ImageOptions{},
		)
	})
	p.trackPress(box)
}

// trackPress 区分点击与拖动：按下后位移不超过阈值算点击；
// 超过阈值后把移动交给系统窗口拖动，保证跟手，松手才返回。
func (p *Pet) trackPress(box ui.Element) {
	_, held := box.PressedButton()
	if held && !p.pressActive {
		if time.Now().Before(p.suppressUntil) {
			return
		}
		p.pressActive = true
		p.dragMoved = false
		p.pressCursor = mygo.Screen.CursorScreenPoint()
		p.pressX, p.pressY = p.win.Position()
	}
	if !held {
		if p.pressActive {
			p.release()
		}
		return
	}

	cursor := mygo.Screen.CursorScreenPoint()
	dx := int(cursor.X - p.pressCursor.X)
	dy := int(cursor.Y - p.pressCursor.Y)
	if !p.dragMoved && (abs(dx) > dragThreshold || abs(dy) > dragThreshold) {
		p.dragMoved = true
		// 系统移动循环一直运行到松手，期间窗口由系统绘制与移动。
		if startNativeWindowDrag(p.win.NativeHandle()) {
			p.pressActive = false
			p.dragMoved = false
			// 松手事件可能稍后才到达，短暂屏蔽以防误判点击。
			p.suppressUntil = time.Now().Add(200 * time.Millisecond)
			p.snap()
			p.storePosition()
			return
		}
	}
	if p.dragMoved {
		p.win.SetPosition(p.pressX+dx, p.pressY+dy)
	}
}

// release 处理松手：点击回到 idle 首帧并回调，拖动则吸附并保存位置。
func (p *Pet) release() {
	dragged := p.dragMoved
	p.pressActive = false
	p.dragMoved = false
	if !dragged {
		p.started = time.Now()
		if p.onClick != nil {
			p.onClick()
		}
		return
	}
	p.snap()
	p.storePosition()
}

// snap 把窗口贴到工作区边缘。
func (p *Pet) snap() {
	if p.win.IsDestroyed() {
		return
	}
	x, y := p.win.Position()
	area := p.workArea(x, y)
	if area.Width <= 0 || area.Height <= 0 {
		return
	}

	nx, ny := x, y
	if abs(x-area.X) <= snapDistance {
		nx = area.X
	}
	if abs(x+p.width-(area.X+area.Width)) <= snapDistance {
		nx = area.X + area.Width - p.width
	}
	if abs(y-area.Y) <= snapDistance {
		ny = area.Y
	}
	if abs(y+p.height-(area.Y+area.Height)) <= snapDistance {
		ny = area.Y + area.Height - p.height
	}
	if nx != x || ny != y {
		p.win.SetPosition(nx, ny)
	}
}

// storePosition 把当前窗口位置换算成归一化坐标交给调用方持久化。
func (p *Pet) storePosition() {
	if p.savePosition == nil || p.win.IsDestroyed() {
		return
	}
	x, y := p.win.Position()
	area := p.workArea(x, y)
	if area.Width <= 0 || area.Height <= 0 {
		return
	}
	p.savePosition(
		float64(x-area.X)/float64(area.Width),
		float64(y-area.Y)/float64(area.Height),
	)
}

// workArea 返回窗口所在显示器的工作区。
func (p *Pet) workArea(x, y int) mygo.Rectangle {
	return mygo.Screen.DisplayMatching(mygo.Rectangle{
		X: x, Y: y, Width: p.width, Height: p.height,
	}).WorkArea
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
