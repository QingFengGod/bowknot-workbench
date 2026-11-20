package pet

import (
	"embed"
	"fmt"
	"path"
)

// builtinFS 是内置形象素材；官方形象随二进制分发，不落到可写目录。
//
//go:embed assets/bowknot
var builtinFS embed.FS

// Bundle 是加载完成的一套形象素材：解析后的规格与图集原始字节。
type Bundle struct {
	Spec Spec
	// Atlases 以 pet.json 中图集的 file 为键，交给 UI 层解码。
	Atlases map[string][]byte
}

// Builtin 读取内置形象。id 对应 assets 目录下的子目录名。
func Builtin(id string) (*Bundle, error) {
	dir := path.Join("assets", id)
	data, err := builtinFS.ReadFile(path.Join(dir, "pet.json"))
	if err != nil {
		return nil, fmt.Errorf("读取形象描述失败: %w", err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{Spec: spec, Atlases: make(map[string][]byte, len(spec.Atlases))}
	for _, atlas := range spec.Atlases {
		raw, err := builtinFS.ReadFile(path.Join(dir, atlas.File))
		if err != nil {
			return nil, fmt.Errorf("读取图集 %s 失败: %w", atlas.File, err)
		}
		bundle.Atlases[atlas.File] = raw
	}
	return bundle, nil
}
