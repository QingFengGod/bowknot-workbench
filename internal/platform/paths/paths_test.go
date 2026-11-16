package paths

import "testing"

func TestCheckWritable(t *testing.T) {
	p := Paths{
		Root: t.TempDir(),
		Logs: t.TempDir(),
		Temp: t.TempDir(),
	}
	if err := p.CheckWritable(); err != nil {
		t.Fatalf("可写目录自检失败：%v", err)
	}

	// 未创建的目录应报错，提示提前暴露权限问题。
	missing := p
	missing.Temp = p.Temp + "/not-exist"
	if err := missing.CheckWritable(); err == nil {
		t.Fatal("不可写目录应自检失败")
	}
}
