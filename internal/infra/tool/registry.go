package tool

import "sync"

// Registry 是按 ToolSpec.ID 索引的线程安全工具注册表。
// 应用与插件的安装、卸载通过 ReplaceSource / UnregisterSource 保持同步。
type Registry struct {
	mu    sync.RWMutex
	items map[string]ToolSpec
}

func NewRegistry() *Registry {
	return &Registry{items: map[string]ToolSpec{}}
}

// Register 新增或覆盖工具条目。
func (r *Registry) Register(specs ...ToolSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, spec := range specs {
		r.items[spec.ID] = spec
	}
}

// UnregisterSource 移除指定来源（app:<id> 或 plugin:<id>）的全部工具。
func (r *Registry) UnregisterSource(source, sourceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, spec := range r.items {
		if spec.Source == source && spec.SourceID == sourceID {
			delete(r.items, id)
		}
	}
}

// ReplaceSource 用 specs 全量替换指定来源的条目，用于安装后的整体同步。
func (r *Registry) ReplaceSource(source string, specs []ToolSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, spec := range r.items {
		if spec.Source == source {
			delete(r.items, id)
		}
	}
	for _, spec := range specs {
		r.items[spec.ID] = spec
	}
}

// Lookup 按 ID 查找工具。
func (r *Registry) Lookup(id string) (ToolSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.items[id]
	return spec, ok
}

// List 返回全部工具的快照，调用方可安全持有。
func (r *Registry) List() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolSpec, 0, len(r.items))
	for _, spec := range r.items {
		out = append(out, spec)
	}
	return out
}
