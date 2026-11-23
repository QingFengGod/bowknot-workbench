package components

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"bowknot-workbench/internal/contract"
)

// maxPreviewBytes 限制文本预览读取量，避免超大文件拖慢界面。
const maxPreviewBytes = 256 << 10

var artifactMarkdown = &markdown.Options{
	Theme: &markdown.Theme{LineHeight: 1.7},
}

// ArtifactPreviewData 是打开产物时一次性读取的预览数据。
type ArtifactPreviewData struct {
	Text      string
	Image     ui.ImageSource
	Truncated bool
	Err       string
}

// LoadArtifactPreview 读取并解码产物文件；页面打开产物时调用一次。
func LoadArtifactPreview(detail contract.ArtifactDetail) ArtifactPreviewData {
	if detail.Kind == contract.ArtifactLink {
		return ArtifactPreviewData{}
	}
	if detail.Path == "" {
		return ArtifactPreviewData{Err: "文件已丢失"}
	}

	file, err := os.Open(detail.Path)
	if err != nil {
		return ArtifactPreviewData{Err: "文件已丢失"}
	}
	defer file.Close()

	if detail.Kind == contract.ArtifactImage {
		data, err := io.ReadAll(file)
		if err != nil {
			return ArtifactPreviewData{Err: "读取图片失败"}
		}
		source, err := ui.DecodeImage(data)
		if err != nil {
			return ArtifactPreviewData{Err: "图片解码失败"}
		}
		return ArtifactPreviewData{Image: source}
	}
	if detail.Kind == contract.ArtifactFile {
		return ArtifactPreviewData{}
	}

	data, err := io.ReadAll(io.LimitReader(file, maxPreviewBytes+1))
	if err != nil {
		return ArtifactPreviewData{Err: "读取文件失败"}
	}
	truncated := len(data) > maxPreviewBytes
	if truncated {
		data = data[:maxPreviewBytes]
	}
	return ArtifactPreviewData{
		Text:      strings.ToValidUTF8(string(data), ""),
		Truncated: truncated,
	}
}

// ArtifactPreview 按类型渲染预览内容。
func ArtifactPreview(c *ui.Context, detail contract.ArtifactDetail, data ArtifactPreviewData) {
	t := c.Theme()

	if detail.Kind == contract.ArtifactLink {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, "链接文件").FontSize(12).Bold()
			ui.Text(c, detail.URI).FontSize(12).TextColor(t.Accent)
			ui.Text(c, "出于安全考虑，链接不会在应用内加载。").
				FontSize(11).
				TextColor(t.TextMuted)
		})
		return
	}

	if data.Err != "" {
		ui.Text(c, data.Err).FontSize(12).TextColor(t.Danger)
		return
	}

	switch detail.Kind {
	case contract.ArtifactImage:
		if data.Image == nil {
			ui.Text(c, "图片无法显示").FontSize(12).TextColor(t.TextMuted)
			return
		}
		width, height := ui.ImageSize(data.Image)
		const maxWidth, maxHeight = 560.0, 640.0
		scale := 1.0
		if width > maxWidth {
			scale = math.Min(scale, maxWidth/float64(width))
		}
		if height > maxHeight {
			scale = math.Min(scale, maxHeight/float64(height))
		}
		ui.Image(c, data.Image).
			Width(float32(float64(width) * scale)).
			Height(float32(float64(height) * scale)).
			Fit(ui.Contain)

	case contract.ArtifactMarkdown:
		markdown.View(c, data.Text, artifactMarkdown)

	case contract.ArtifactCode, contract.ArtifactHTML:
		if detail.Kind == contract.ArtifactHTML {
			ui.Text(c, "网页文件只展示源码，请用浏览器打开查看效果。").
				FontSize(11).
				TextColor(t.TextMuted)
		}
		markdown.View(c, codeFence(data.Text, codeLanguage(detail.Filename)), artifactMarkdown)

	case contract.ArtifactFile:
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			ui.Text(c, "该类型暂不支持内嵌预览。").FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "可以使用下方「打开」交给系统应用处理。").FontSize(11).TextColor(t.TextMuted)
		})

	default:
		ui.Text(c, data.Text).FontSize(12).TextColor(t.Text).LineHeight(1.6)
	}

	if data.Truncated {
		Chip(c, "内容过长，仅显示前 256 KB", ToneNeutral)
	}
}

// codeFence 生成不会与正文冲突的代码围栏。
func codeFence(text, language string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	if language != "" {
		return fence + language + "\n" + text + "\n" + fence
	}
	return fence + "\n" + text + "\n" + fence
}

func codeLanguage(filename string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	return ext
}
