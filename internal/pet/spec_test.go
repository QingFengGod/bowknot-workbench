package pet

import (
	"testing"
	"time"
)

func TestBuiltinBowknot(t *testing.T) {
	bundle, err := Builtin("bowknot")
	if err != nil {
		t.Fatalf("读取内置形象失败: %v", err)
	}
	if len(bundle.Atlases) != len(bundle.Spec.Atlases) {
		t.Fatalf("图集数量不一致: %d", len(bundle.Atlases))
	}
	idle, ok := bundle.Spec.Animations["idle"]
	if !ok || len(idle.Frames) == 0 {
		t.Fatal("缺少 idle 动作")
	}
	if got := idle.FrameAt(0); got != 0 {
		t.Errorf("首帧 = %d，期望 0", got)
	}
	if got, want := idle.FrameAt(125*time.Millisecond), 1; got != want {
		t.Errorf("125ms 帧 = %d，期望 %d", got, want)
	}
	if got := idle.FrameAt(idle.Duration()); got != 0 {
		t.Errorf("循环回到首帧时 = %d，期望 0", got)
	}
	if got, want := idle.FrameAt(idle.Duration()-time.Millisecond), len(idle.Frames)-1; got != want {
		t.Errorf("末帧 = %d，期望 %d", got, want)
	}
}

func TestParseSpecRejectsOutOfRange(t *testing.T) {
	_, err := ParseSpec([]byte(`{
		"id": "bad",
		"display": {"width": 10, "height": 10},
		"source": {"width": 10, "height": 10},
		"atlases": [{"file": "a.png", "width": 10, "height": 10}],
		"animations": {"idle": {"fps": 8, "loop": true, "frames": [
			{"atlas": 0, "x": 5, "y": 0, "w": 10, "h": 10}
		]}}
	}`))
	if err == nil {
		t.Fatal("越界帧未被拒绝")
	}
}
