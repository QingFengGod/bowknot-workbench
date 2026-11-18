package llm

import (
	"context"
	"strings"
)

// Route 描述一个内部模型 key 对应的上游连接信息。
type Route struct {
	// ProviderID 用于复用同一服务商的连接。
	ProviderID string
	BaseURL    string
	APIKey     string
	// ModelID 是上游请求真正使用的模型名。
	ModelID string
}

// RouterOptions 描述多服务商路由配置。
type RouterOptions struct {
	// DefaultKey 是请求未指定模型时使用的内部 key。
	DefaultKey string
	Routes     map[string]Route
	// Order 是路由 key 的稳定顺序，用于默认 key 缺失时的兜底。
	Order []string
	// Models 是向调用方展示的已启用模型列表。
	Models []ModelInfo
}

// NewRouter 按内部模型 key 把请求分发到不同服务商；没有路由时回退到 Fake。
func NewRouter(opts RouterOptions) Provider {
	if len(opts.Routes) == 0 {
		return NewFake(FakeOptions{})
	}

	r := &router{
		defaultKey: strings.TrimSpace(opts.DefaultKey),
		routes:     make(map[string]Route, len(opts.Routes)),
		order:      append([]string(nil), opts.Order...),
		models:     append([]ModelInfo(nil), opts.Models...),
		conns:      map[string]Provider{},
	}
	for key, route := range opts.Routes {
		r.routes[key] = route
	}
	// Order 可能缺少某些 key（例如手工构造的测试配置），补全后再构建连接。
	seen := make(map[string]bool, len(opts.Routes))
	for _, key := range r.order {
		if _, ok := r.routes[key]; ok {
			seen[key] = true
		}
	}
	for key := range r.routes {
		if !seen[key] {
			r.order = append(r.order, key)
		}
	}

	for _, key := range r.order {
		route := r.routes[key]
		if _, ok := r.conns[route.ProviderID]; ok {
			continue
		}
		r.conns[route.ProviderID] = WithRetry(newOpenAI(Options{
			Provider: ProviderOpenAICompatible,
			BaseURL:  route.BaseURL,
			APIKey:   route.APIKey,
		}), RetryOptions{})
	}
	return r
}

// ProviderOpenAICompatible 与 config 包保持一致的协议标识。
const ProviderOpenAICompatible = "openai-compatible"

type router struct {
	defaultKey string
	routes     map[string]Route
	order      []string
	models     []ModelInfo
	conns      map[string]Provider
}

func (r *router) Stream(ctx context.Context, req ChatRequest) (<-chan Chunk, error) {
	conn, route, err := r.pick(req.Model)
	if err != nil {
		return nil, err
	}
	req.Model = route.ModelID
	return conn.Stream(ctx, req)
}

func (r *router) Complete(ctx context.Context, req ChatRequest) (string, error) {
	conn, route, err := r.pick(req.Model)
	if err != nil {
		return "", err
	}
	req.Model = route.ModelID
	return conn.Complete(ctx, req)
}

func (r *router) Models(context.Context) ([]ModelInfo, error) {
	return append([]ModelInfo(nil), r.models...), nil
}

// pick 解析内部 key 并返回对应连接；未命中时报错而不是静默改用其它模型。
func (r *router) pick(model string) (Provider, Route, error) {
	key := strings.TrimSpace(model)
	if key == "" {
		key = r.defaultKey
	}
	if key == "" && len(r.order) > 0 {
		key = r.order[0]
	}

	route, ok := r.routes[key]
	if !ok {
		message := "所选模型未配置或已停用"
		if key != "" {
			message += "：" + key
		}
		return nil, Route{}, &Error{Kind: KindServer, Message: message}
	}
	conn := r.conns[route.ProviderID]
	if conn == nil {
		return nil, Route{}, &Error{Kind: KindServer, Message: "模型服务连接不可用：" + key}
	}
	return conn, route, nil
}
