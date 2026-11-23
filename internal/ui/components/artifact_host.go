package components

import (
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
)

// ArtifactHost 统一管理文件预览抽屉与删除确认。
// 页面持有实例，打开时调用 Open，每帧调用 Render，避免各页面重复维护
// 打开状态、预览数据与删除确认框。
type ArtifactHost struct {
	service    contract.ArtifactService
	viewer     ArtifactViewer
	open       bool
	deleteOpen bool
}

func NewArtifactHost(service contract.ArtifactService) *ArtifactHost {
	return &ArtifactHost{service: service}
}

// Open 读取文件详情并展开预览抽屉。
func (h *ArtifactHost) Open(id string) {
	if h == nil || h.service == nil || id == "" {
		return
	}
	h.viewer.Open(h.service, id)
	h.open = true
}

// Close 收起抽屉；保留预览数据，避免收起动画期间闪出空状态。
func (h *ArtifactHost) Close() {
	if h == nil {
		return
	}
	h.open = false
}

// Visible 返回抽屉当前是否展开。
func (h *ArtifactHost) Visible() bool {
	return h != nil && h.open
}

// ArtifactHostProps 是页面差异化的行为。
type ArtifactHostProps struct {
	// OnReveal 跳转到文件来源消息；为 nil 时不显示入口。
	OnReveal func(messageID string)
	// OnDelete 由页面执行删除；成功后页面应调用 Close。
	OnDelete func(id string)
}

// Render 渲染预览抽屉与删除确认框。
func (h *ArtifactHost) Render(c *ui.Context, props ArtifactHostProps) {
	if h == nil {
		return
	}

	ArtifactDrawer(c, ArtifactDrawerProps{
		Open:   &h.open,
		Viewer: h.viewer,
		OnReveal: func(messageID string) {
			h.Close()
			if props.OnReveal != nil {
				props.OnReveal(messageID)
			}
		},
		OnDelete: func() {
			h.deleteOpen = true
		},
	})
	ArtifactDeleteDialog(c, &h.deleteOpen, h.viewer.Detail.Title, h.viewer.Detail.External, func() {
		if props.OnDelete != nil {
			props.OnDelete(h.viewer.Detail.ID)
		}
	})
}
