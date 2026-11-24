package components

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/ui/format"
	"bowknot-workbench/internal/ui/theme"
)

// maxPendingImageBytes 是待发送缩略图解码的上限，超出的图片只显示文件条。
const maxPendingImageBytes = 20 << 20

// maxPastedImageBytes 与后端图片上限保持一致，粘贴时提前拒绝超大截图。
const maxPastedImageBytes = 5 << 20

// PendingAttachment 是输入区里已选中、尚未发送的附件。
// Image 只在图片解码成功时非空，用于输入区缩略图。
type PendingAttachment struct {
	Path  string
	Name  string
	Size  int64
	Kind  contract.ArtifactKind
	Image ui.ImageSource
	// Temp 表示文件来自剪贴板暂存，清理附件时连同临时目录一起删除。
	Temp bool
}

// DiscardPending 清理待发送附件的临时文件。
func DiscardPending(items []PendingAttachment) {
	for _, item := range items {
		if item.Temp && item.Path != "" {
			_ = os.RemoveAll(filepath.Dir(item.Path))
		}
	}
}

// LoadPendingAttachments 把用户选中的路径转成待发送附件。
// 返回可加入列表的附件与逐条错误文案，由页面决定如何提示。
func LoadPendingAttachments(paths []string) ([]PendingAttachment, []string) {
	out := make([]PendingAttachment, 0, len(paths))
	errs := make([]string, 0)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			errs = append(errs, "无法读取："+filepath.Base(path))
			continue
		}
		name := filepath.Base(path)
		kind := attachmentKindOf(name)
		item := PendingAttachment{Path: path, Name: name, Size: info.Size(), Kind: kind}
		if kind == contract.ArtifactImage && info.Size() <= maxPendingImageBytes {
			item.Image = decodeAttachmentImage(path)
		}
		out = append(out, item)
	}
	return out, errs
}

// PendingFromClipboard 把剪贴板里的图片或文件转成待发送附件。
// 图片优先于文件列表：截图工具常同时提供两者。
func PendingFromClipboard(data transfer.Data) ([]PendingAttachment, []string) {
	if len(data.Items()) == 0 {
		return nil, []string{"剪贴板里没有可添加的图片或文件"}
	}
	if png, err := data.Read(transfer.PNG); err == nil && len(png) > 0 {
		item, err := pendingFromImageBytes(png)
		if err != nil {
			return nil, []string{err.Error()}
		}
		return []PendingAttachment{item}, nil
	}
	files, err := data.Files()
	if err != nil || len(files) == 0 {
		return nil, []string{"剪贴板里没有可添加的图片或文件"}
	}
	return LoadPendingAttachments(files)
}

// pendingFromImageBytes 把剪贴板图片写到系统临时目录，供统一的上传流程读取。
// 文件名为 image.png，避免卡片显示随机临时名。
func pendingFromImageBytes(data []byte) (PendingAttachment, error) {
	if len(data) > maxPastedImageBytes {
		return PendingAttachment{}, errors.New("粘贴的图片超过 5 MB")
	}
	dir, err := os.MkdirTemp("", "bowknot-paste-")
	if err != nil {
		return PendingAttachment{}, errors.New("无法暂存剪贴板图片")
	}
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return PendingAttachment{}, errors.New("无法暂存剪贴板图片")
	}
	source, err := ui.DecodeImage(data)
	if err != nil {
		return PendingAttachment{}, errors.New("剪贴板里的图片无法解析")
	}
	return PendingAttachment{
		Path:  path,
		Name:  "image.png",
		Size:  int64(len(data)),
		Kind:  contract.ArtifactImage,
		Image: source,
		Temp:  true,
	}, nil
}

func attachmentKindOf(name string) contract.ArtifactKind {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return contract.ArtifactImage
	case ".md", ".markdown":
		return contract.ArtifactMarkdown
	case ".html", ".htm":
		return contract.ArtifactHTML
	case ".txt", "":
		return contract.ArtifactText
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".cc", ".cpp", ".h",
		".rs", ".rb", ".php", ".sh", ".sql", ".yaml", ".yml", ".json", ".toml", ".xml",
		".css", ".vue", ".kt", ".swift":
		return contract.ArtifactCode
	default:
		return contract.ArtifactFile
	}
}

func decodeAttachmentImage(path string) ui.ImageSource {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxPendingImageBytes))
	if err != nil {
		return nil
	}
	source, err := ui.DecodeImage(data)
	if err != nil {
		return nil
	}
	return source
}

// AttachButton 是输入区左侧的附件按钮；enabled 为 false 时不可点。
func AttachButton(c *ui.Context, enabled bool, onClick func()) {
	t := c.Theme()

	b := ui.ButtonBase(c).
		Width(32).
		Height(32).
		Radius(16).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Background(ui.Transparent).
		TextColor(t.TextMuted)
	if b.Hovered() && enabled {
		b.Background(t.SurfaceHover).TextColor(t.Accent)
	}
	if !enabled {
		b.Opacity(0.45)
	}
	b.Children(func() {
		Icon(c, IconPaperclip, 16)
	})
	if b.Tooltip("添加图片或文件").Clicked() && enabled && onClick != nil {
		onClick()
	}
}

// AttachmentChipProps 描述一个附件卡片：左侧缩略图，右侧名称与「类型 · 大小」。
// OnOpen 非空时可点击，OnRemove 非空时悬停显示移除按钮。
type AttachmentChipProps struct {
	Name     string
	Kind     contract.ArtifactKind
	Size     int64
	Image    ui.ImageSource
	Meta     string
	OnOpen   func()
	OnRemove func()
}

// AttachmentChip 是输入区与消息里共用的附件卡片。
func AttachmentChip(c *ui.Context, p AttachmentChipProps) {
	t := c.Theme()

	// 卡片必须是横向容器，Box 默认纵向排列会让缩略图与文字上下堆叠。
	var chip ui.Element
	if p.OnOpen != nil {
		chip = ui.ButtonBase(c)
	} else {
		chip = ui.Row(c)
	}
	chip.Padding(8).
		Gap(8).
		MaxWidth(260).
		Radius(theme.RadiusSM).
		BorderWidth(1).
		BorderColor(t.Border).
		Background(t.SurfaceHover).
		AlignItems(ui.Center)
	if chip.Hovered() {
		chip.Background(t.SurfacePressed).BorderColor(t.Accent.Alpha(0.5))
	}

	meta := p.Meta
	if meta == "" {
		meta = attachmentMeta(p.Kind, p.Name, p.Size)
	}

	chip.Children(func() {
		thumb := ui.Box(c).
			Width(34).
			Height(34).
			Radius(theme.RadiusXS).
			Clip().
			Background(t.Surface).
			AlignItems(ui.Center).
			Justify(ui.Center)
		thumb.Children(func() {
			if p.Image != nil {
				ui.Image(c, p.Image).Width(34).Height(34).Fit(ui.Cover)
				return
			}
			Icon(c, ArtifactIcon(p.Kind), 16).TextColor(t.Accent)
		})

		ui.Column(c).Gap(1).MaxWidth(190).Children(func() {
			ui.Text(c, p.Name).FontSize(13).MaxLines(1)
			ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).MaxLines(1)
		})

		if p.OnRemove == nil {
			return
		}
		remove := ui.ButtonBase(c).
			Width(20).
			Height(20).
			Radius(10).
			AlignItems(ui.Center).
			Justify(ui.Center).
			Background(ui.Transparent).
			TextColor(t.TextMuted)
		// 用透明度占位，悬停时出现，避免卡片尺寸跳动。
		if !chip.Hovered() {
			remove.Opacity(0)
		}
		if remove.Hovered() {
			remove.Background(t.Danger.Alpha(0.12)).TextColor(t.Danger)
		}
		remove.Children(func() {
			Icon(c, IconClose, 11)
		})
		if remove.Tooltip("移除附件").Clicked() {
			p.OnRemove()
		}
	})

	if p.OnOpen != nil && chip.Clicked() {
		p.OnOpen()
	}
}

// attachmentMeta 生成「类型 · 大小」，例如 PNG · 53.4 KB。
func attachmentMeta(kind contract.ArtifactKind, name string, size int64) string {
	label := strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
	if label == "" {
		switch kind {
		case contract.ArtifactImage:
			label = "图片"
		case contract.ArtifactMarkdown:
			label = "Markdown"
		case contract.ArtifactCode:
			label = "代码"
		case contract.ArtifactHTML:
			label = "HTML"
		case contract.ArtifactLink:
			label = "链接"
		default:
			label = "文件"
		}
	}
	if size <= 0 {
		return label
	}
	return label + " · " + format.Size(size)
}

// AttachmentTray 渲染待发送附件：图片显示缩略图，其它显示类型图标。
func AttachmentTray(c *ui.Context, items []PendingAttachment, onRemove func(index int)) {
	if len(items) == 0 {
		return
	}

	ui.Row(c).FillWidth().Wrap().Gap(6).Children(func() {
		for index, item := range items {
			index := index
			item := item
			AttachmentChip(c, AttachmentChipProps{
				Name:  item.Name,
				Kind:  item.Kind,
				Size:  item.Size,
				Image: item.Image,
				OnRemove: func() {
					if onRemove != nil {
						onRemove(index)
					}
				},
			})
		}
	})
}

// MessageImage 是一条消息里的图片附件。
type MessageImage struct {
	ID     string
	Title  string
	Source ui.ImageSource
}

// AttachmentImages 渲染用户消息里的图片缩略图，点击打开产物预览。
func AttachmentImages(c *ui.Context, images []MessageImage, onOpen func(id string)) {
	if len(images) == 0 {
		return
	}
	t := c.Theme()

	ui.Row(c).Wrap().Gap(6).Children(func() {
		for _, item := range images {
			item := item
			box := ui.ButtonBase(c).
				Width(132).
				Height(96).
				Radius(theme.RadiusSM).
				Clip().
				BorderWidth(1).
				BorderColor(t.Border)
			if box.Hovered() {
				box.BorderColor(t.Accent)
			}
			box.Children(func() {
				if item.Source != nil {
					ui.Image(c, item.Source).Width(132).Height(96).Fit(ui.Cover)
					return
				}
				placeholder := ui.Box(c).
					Width(132).
					Height(96).
					Background(t.SurfaceHover).
					AlignItems(ui.Center).
					Justify(ui.Center)
				placeholder.Children(func() {
					Icon(c, IconImage, 20).TextColor(t.TextMuted)
				})
			})
			if box.Clicked() && onOpen != nil {
				onOpen(item.ID)
			}
		}
	})
}
