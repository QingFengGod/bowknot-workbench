// Package pet 提供桌宠形象素材的规格解析与动画取帧逻辑，不依赖 UI。
package pet

import (
	"encoding/json"
	"fmt"
	"time"
)

// Spec 是 pet.json 的内容：素材尺寸、锚点、图集与动作帧表。
type Spec struct {
	ID         string               `json:"id"`
	Version    int                  `json:"version"`
	Display    Size                 `json:"display"`
	Source     Size                 `json:"source"`
	Anchor     Point                `json:"anchor"`
	Atlases    []Atlas              `json:"atlases"`
	Animations map[string]Animation `json:"animations"`
	HitRects   []Rect               `json:"hitRects"`
}

// Size 是宽高，单位由使用方决定。
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Point 是素材坐标系里的一个点。
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Rect 是素材坐标系里的矩形。
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"w"`
	Height int `json:"h"`
}

// Atlas 是一张图集的文件名与像素尺寸。
type Atlas struct {
	File   string `json:"file"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Animation 是一个动作的帧表；Frames 按顺序循环或只播一次。
type Animation struct {
	FPS    float64 `json:"fps"`
	Loop   bool    `json:"loop"`
	Frames []Frame `json:"frames"`
}

// Frame 是图集上的一帧；Duration 为毫秒，缺省按 FPS 推算。
type Frame struct {
	Atlas    int `json:"atlas"`
	X        int `json:"x"`
	Y        int `json:"y"`
	Width    int `json:"w"`
	Height   int `json:"h"`
	Duration int `json:"duration"`
}

// ParseSpec 解析并校验 pet.json。校验失败时返回中文错误，供日志展示。
func ParseSpec(data []byte) (Spec, error) {
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return Spec{}, fmt.Errorf("解析形象描述失败: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// Validate 校验素材尺寸、图集与帧矩形是否自洽。
func (s Spec) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("形象描述缺少 id")
	}
	if s.Display.Width <= 0 || s.Display.Height <= 0 {
		return fmt.Errorf("形象描述缺少显示尺寸")
	}
	if s.Source.Width <= 0 || s.Source.Height <= 0 {
		return fmt.Errorf("形象描述缺少素材尺寸")
	}
	if s.Anchor.X < 0 || s.Anchor.X > s.Source.Width || s.Anchor.Y < 0 || s.Anchor.Y > s.Source.Height {
		return fmt.Errorf("形象锚点超出素材范围")
	}
	if len(s.Atlases) == 0 {
		return fmt.Errorf("形象描述缺少图集")
	}
	if len(s.Animations) == 0 {
		return fmt.Errorf("形象描述缺少动作")
	}
	for name, anim := range s.Animations {
		if len(anim.Frames) == 0 {
			return fmt.Errorf("动作 %s 没有帧", name)
		}
		if anim.FPS <= 0 && anim.totalMillis() <= 0 {
			return fmt.Errorf("动作 %s 缺少帧时长", name)
		}
		for i, frame := range anim.Frames {
			if frame.Atlas < 0 || frame.Atlas >= len(s.Atlases) {
				return fmt.Errorf("动作 %s 的第 %d 帧指向不存在的图集", name, i)
			}
			atlas := s.Atlases[frame.Atlas]
			if frame.Width <= 0 || frame.Height <= 0 ||
				frame.X < 0 || frame.Y < 0 ||
				frame.X+frame.Width > atlas.Width || frame.Y+frame.Height > atlas.Height {
				return fmt.Errorf("动作 %s 的第 %d 帧超出图集范围", name, i)
			}
		}
	}
	for _, r := range s.HitRects {
		if r.Width <= 0 || r.Height <= 0 ||
			r.X < 0 || r.Y < 0 ||
			r.X+r.Width > s.Source.Width || r.Y+r.Height > s.Source.Height {
			return fmt.Errorf("命中区域超出素材范围")
		}
	}
	return nil
}

// FrameAt 返回 elapsed 时刻应显示的帧序号。循环动作按总时长取模，
// 非循环动作停在一帧上。
func (a Animation) FrameAt(elapsed time.Duration) int {
	if len(a.Frames) == 0 {
		return 0
	}
	total := a.totalMillis()
	if total <= 0 {
		return 0
	}
	ms := elapsed.Milliseconds()
	if a.Loop {
		ms %= int64(total)
	} else if ms >= int64(total) {
		return len(a.Frames) - 1
	}
	passed := 0
	for i, frame := range a.Frames {
		passed += a.frameMillis(frame)
		if ms < int64(passed) {
			return i
		}
	}
	return len(a.Frames) - 1
}

// Duration 返回动作一轮的总时长。
func (a Animation) Duration() time.Duration {
	return time.Duration(a.totalMillis()) * time.Millisecond
}

func (a Animation) totalMillis() int {
	total := 0
	for _, frame := range a.Frames {
		total += a.frameMillis(frame)
	}
	return total
}

func (a Animation) frameMillis(frame Frame) int {
	if frame.Duration > 0 {
		return frame.Duration
	}
	if a.FPS <= 0 {
		return 0
	}
	return int(1000/a.FPS + 0.5)
}
