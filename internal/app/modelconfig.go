package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/infra/config"
	"bowknot-workbench/internal/infra/llm"
	"bowknot-workbench/internal/platform/id"
	"bowknot-workbench/internal/store"
)

// ModelConfigService 负责多服务商与模型配置的读写，数据落在 config.json。
// 配置修改不会热重建网关，保存后需要重启才能生效。
type ModelConfigService struct {
	mu     sync.Mutex
	path   string
	logger *slog.Logger
}

func NewModelConfigService(path string, logger *slog.Logger) *ModelConfigService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ModelConfigService{path: path, logger: logger}
}

func (s *ModelConfigService) load() (config.Config, error) {
	cfg, err := config.Load(s.path)
	if err != nil {
		return config.Config{}, fmt.Errorf("读取模型配置失败：%w", err)
	}
	return cfg, nil
}

func (s *ModelConfigService) Providers() []contract.ProviderConfig {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		s.logger.Error("读取服务商配置失败", "error", err)
		return nil
	}

	counts := make(map[string]int, len(cfg.Models))
	for _, item := range cfg.Models {
		counts[item.ProviderID]++
	}
	out := make([]contract.ProviderConfig, 0, len(cfg.Providers))
	for _, item := range cfg.Providers {
		out = append(out, contract.ProviderConfig{
			ID:         item.ID,
			Name:       item.Name,
			Type:       item.Type,
			BaseURL:    item.BaseURL,
			APIKey:     config.MaskKey(item.APIKey),
			HasKey:     strings.TrimSpace(item.APIKey) != "",
			Enabled:    item.Enabled,
			ModelCount: counts[item.ID],
		})
	}
	return out
}

func (s *ModelConfigService) SaveProvider(draft contract.ProviderConfig) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft.Name = strings.TrimSpace(draft.Name)
	if draft.Name == "" {
		return "", errors.New("服务商名称不能为空")
	}

	cfg, err := s.load()
	if err != nil {
		return "", err
	}

	providerID := strings.TrimSpace(draft.ID)
	if providerID == "" {
		providerID = id.New()
	}

	index := -1
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == providerID {
			index = i
			break
		}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(draft.BaseURL), "/")
	if index >= 0 {
		item := &cfg.Providers[index]
		item.Name = draft.Name
		item.BaseURL = baseURL
		item.Enabled = draft.Enabled
		item.Type = config.ProviderOpenAICompatible
		switch {
		case draft.ClearKey:
			item.APIKey = ""
		case strings.TrimSpace(draft.APIKeyInput) != "":
			item.APIKey = strings.TrimSpace(draft.APIKeyInput)
		}
	} else {
		cfg.Providers = append(cfg.Providers, config.Provider{
			ID:      providerID,
			Name:    draft.Name,
			Type:    config.ProviderOpenAICompatible,
			BaseURL: baseURL,
			APIKey:  strings.TrimSpace(draft.APIKeyInput),
			Enabled: draft.Enabled,
		})
	}

	ensureDefaultModel(&cfg)
	if err := config.Save(s.path, cfg); err != nil {
		return "", err
	}
	return providerID, nil
}

func (s *ModelConfigService) DeleteProvider(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		return err
	}

	count := 0
	for _, item := range cfg.Models {
		if item.ProviderID == providerID {
			count++
		}
	}
	if count > 0 {
		return fmt.Errorf("该服务下还有 %d 个模型，请先删除模型", count)
	}

	out := cfg.Providers[:0]
	found := false
	for _, item := range cfg.Providers {
		if item.ID == providerID {
			found = true
			continue
		}
		out = append(out, item)
	}
	if !found {
		return errors.New("服务商不存在")
	}
	cfg.Providers = out
	ensureDefaultModel(&cfg)
	return config.Save(s.path, cfg)
}

func (s *ModelConfigService) TestProvider(draft contract.ProviderConfig) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(draft.BaseURL), "/")
	if baseURL == "" {
		return "", errors.New("请先填写 BaseURL")
	}

	apiKey := strings.TrimSpace(draft.APIKeyInput)
	if apiKey == "" {
		apiKey = s.storedKey(draft.ID)
	}

	provider := llm.New(llm.Options{
		Provider: config.ProviderOpenAICompatible,
		BaseURL:  baseURL,
		APIKey:   apiKey,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	models, err := provider.Models(ctx)
	if err != nil {
		return "", fmt.Errorf("连接失败：%w", err)
	}
	return fmt.Sprintf("连接成功，可用模型 %d 个", len(models)), nil
}

func (s *ModelConfigService) FetchModels(providerID string) ([]contract.ChatModel, error) {
	s.mu.Lock()
	cfg, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	provider, ok := cfg.ProviderByID(strings.TrimSpace(providerID))
	if !ok {
		return nil, errors.New("服务商不存在")
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return nil, errors.New("该服务商还没有填写 BaseURL")
	}

	gateway := llm.New(llm.Options{
		Provider: config.ProviderOpenAICompatible,
		BaseURL:  provider.BaseURL,
		APIKey:   provider.APIKey,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	infos, err := gateway.Models(ctx)
	if err != nil {
		return nil, fmt.Errorf("拉取模型失败：%w", err)
	}
	out := make([]contract.ChatModel, 0, len(infos))
	for _, info := range infos {
		modelID := strings.TrimSpace(info.ID)
		if modelID == "" {
			continue
		}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			name = modelID
		}
		out = append(out, contract.ChatModel{ID: modelID, Name: name})
	}
	return out, nil
}

// TTS 返回语音合成配置快照，密钥按服务商同样的规则脱敏。
func (s *ModelConfigService) TTS() contract.TTSSettings {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		s.logger.Error("读取语音合成配置失败", "error", err)
		return contract.TTSSettings{}
	}
	item := cfg.TTS
	return contract.TTSSettings{
		Enabled:    item.Enabled,
		BaseURL:    item.BaseURL,
		APIKey:     config.MaskKey(item.APIKey),
		HasKey:     strings.TrimSpace(item.APIKey) != "",
		ResourceID: item.ResourceID,
		Speaker:    item.Speaker,
		Format:     item.Format,
		SampleRate: item.SampleRate,
		SpeechRate: item.SpeechRate,
	}
}

// SaveTTS 写入语音合成配置；留空密钥表示保持原值。
func (s *ModelConfigService) SaveTTS(draft contract.TTSSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		return err
	}

	key := cfg.TTS.APIKey
	switch {
	case draft.ClearKey:
		key = ""
	case strings.TrimSpace(draft.APIKeyInput) != "":
		key = strings.TrimSpace(draft.APIKeyInput)
	}

	cfg.TTS = config.TTS{
		Enabled:    draft.Enabled,
		BaseURL:    strings.TrimSpace(draft.BaseURL),
		APIKey:     key,
		ResourceID: strings.TrimSpace(draft.ResourceID),
		Speaker:    strings.TrimSpace(draft.Speaker),
		Format:     strings.TrimSpace(draft.Format),
		SampleRate: draft.SampleRate,
		SpeechRate: draft.SpeechRate,
	}
	return config.Save(s.path, cfg)
}

func (s *ModelConfigService) Models() []contract.ModelConfig {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		s.logger.Error("读取模型配置失败", "error", err)
		return nil
	}
	out := make([]contract.ModelConfig, 0, len(cfg.Models))
	for _, item := range cfg.Models {
		out = append(out, contract.ModelConfig{
			Key:             item.Key,
			ProviderID:      item.ProviderID,
			ModelID:         item.ModelID,
			Name:            item.Name,
			Vision:          item.Vision,
			ContextTokens:   item.ContextTokens,
			MaxOutputTokens: item.MaxOutputTokens,
			Enabled:         item.Enabled,
		})
	}
	return out
}

func (s *ModelConfigService) SaveModel(draft contract.ModelConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft.ProviderID = strings.TrimSpace(draft.ProviderID)
	draft.ModelID = strings.TrimSpace(draft.ModelID)
	if draft.ModelID == "" {
		return errors.New("模型 ID 不能为空")
	}
	if strings.Contains(draft.ModelID, "/") {
		return errors.New("模型 ID 不能包含斜杠")
	}
	key := strings.TrimSpace(draft.Key)
	if key == "" {
		key = draft.ProviderID + "/" + draft.ModelID
	}
	if key != draft.ProviderID+"/"+draft.ModelID {
		return errors.New("模型 ID 保存后不可修改，请删除后重新添加")
	}

	cfg, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := cfg.ProviderByID(draft.ProviderID); !ok {
		return errors.New("请选择有效的服务商")
	}

	name := strings.TrimSpace(draft.Name)
	if name == "" {
		name = draft.ModelID
	}

	index := -1
	for i := range cfg.Models {
		if cfg.Models[i].Key == key {
			index = i
			break
		}
	}
	item := config.Model{
		Key:             key,
		ProviderID:      draft.ProviderID,
		ModelID:         draft.ModelID,
		Name:            name,
		Vision:          draft.Vision,
		ContextTokens:   draft.ContextTokens,
		MaxOutputTokens: draft.MaxOutputTokens,
		Enabled:         draft.Enabled,
	}
	if index >= 0 {
		cfg.Models[index] = item
	} else {
		cfg.Models = append(cfg.Models, item)
	}

	ensureDefaultModel(&cfg)
	return config.Save(s.path, cfg)
}

func (s *ModelConfigService) DeleteModel(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		return err
	}

	out := cfg.Models[:0]
	found := false
	for _, item := range cfg.Models {
		if item.Key == key {
			found = true
			continue
		}
		out = append(out, item)
	}
	if !found {
		return errors.New("模型不存在")
	}
	cfg.Models = out
	if cfg.DefaultModelKey == key {
		cfg.DefaultModelKey = ""
	}
	ensureDefaultModel(&cfg)
	return config.Save(s.path, cfg)
}

func (s *ModelConfigService) DefaultModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		return ""
	}
	if model, ok := cfg.DefaultModel(); ok {
		return model.Key
	}
	return ""
}

func (s *ModelConfigService) SetDefaultModel(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		return err
	}
	model, ok := cfg.ModelByKey(strings.TrimSpace(key))
	if !ok {
		return errors.New("模型不存在")
	}
	if !model.Enabled {
		return errors.New("请先启用该模型")
	}
	provider, ok := cfg.ProviderByID(model.ProviderID)
	if !ok || !provider.Enabled {
		return errors.New("该模型所属服务商已停用")
	}
	cfg.DefaultModelKey = model.Key
	return config.Save(s.path, cfg)
}

func (s *ModelConfigService) ChatModels() []contract.ChatModel {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		s.logger.Error("读取模型列表失败", "error", err)
		return nil
	}
	out := make([]contract.ChatModel, 0, len(cfg.Models))
	for _, item := range cfg.EnabledModels() {
		provider, _ := cfg.ProviderByID(item.ProviderID)
		out = append(out, contract.ChatModel{
			ID:              item.Key,
			Name:            item.Name,
			Description:     provider.Name,
			ProviderName:    provider.Name,
			Vision:          item.Vision,
			ContextTokens:   item.ContextTokens,
			MaxOutputTokens: item.MaxOutputTokens,
		})
	}
	return out
}

// LogLevel 返回配置文件中的日志级别；读取失败时返回空字符串。
func (s *ModelConfigService) LogLevel() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.load()
	if err != nil {
		s.logger.Error("读取日志级别失败", "error", err)
		return ""
	}
	return cfg.LogLevel
}

// storedKey 读取已保存服务商的密钥；不存在时返回空。
func (s *ModelConfigService) storedKey(providerID string) string {
	if strings.TrimSpace(providerID) == "" {
		return ""
	}
	cfg, err := s.load()
	if err != nil {
		return ""
	}
	provider, ok := cfg.ProviderByID(providerID)
	if !ok {
		return ""
	}
	return provider.APIKey
}

// ensureDefaultModel 保证默认模型指向启用且服务商可用的模型。
func ensureDefaultModel(cfg *config.Config) {
	current := strings.TrimSpace(cfg.DefaultModelKey)
	if current != "" {
		if model, ok := cfg.ModelByKey(current); ok && model.Enabled {
			if provider, ok := cfg.ProviderByID(model.ProviderID); ok && provider.Enabled {
				return
			}
		}
	}
	enabled := cfg.EnabledModels()
	if len(enabled) == 0 {
		cfg.DefaultModelKey = ""
		return
	}
	cfg.DefaultModelKey = enabled[0].Key
}

// NewProviderFromConfig 按当前配置构建多服务商网关；没有可用服务商时回退 Fake。
func NewProviderFromConfig(cfg config.Config) llm.Provider {
	routes := make(map[string]llm.Route, len(cfg.Models))
	order := make([]string, 0, len(cfg.Models))
	models := make([]llm.ModelInfo, 0, len(cfg.Models))

	for _, item := range cfg.EnabledModels() {
		provider, ok := cfg.ProviderByID(item.ProviderID)
		if !ok {
			continue
		}
		routes[item.Key] = llm.Route{
			ProviderID: provider.ID,
			BaseURL:    provider.BaseURL,
			APIKey:     provider.APIKey,
			ModelID:    item.ModelID,
		}
		order = append(order, item.Key)
		models = append(models, llm.ModelInfo{ID: item.Key, Name: item.Name})
	}
	if len(routes) == 0 {
		return llm.NewFake(llm.FakeOptions{})
	}

	defaultKey := strings.TrimSpace(cfg.DefaultModelKey)
	if _, ok := routes[defaultKey]; !ok {
		defaultKey = order[0]
	}
	return llm.NewRouter(llm.RouterOptions{
		DefaultKey: defaultKey,
		Routes:     routes,
		Order:      order,
		Models:     models,
	})
}

// ReconcileAgentModels 把 Agent 里旧的模型 ID 迁移为内部 key。
// 没有配置任何模型（演示模式）时保持原值不动。
func ReconcileAgentModels(ctx context.Context, st *store.Store, models []contract.ChatModel, defaultKey string, logger *slog.Logger) {
	if st == nil || len(models) == 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	keys := make(map[string]bool, len(models))
	byRaw := make(map[string][]string, len(models))
	for _, item := range models {
		keys[item.ID] = true
		raw := item.ID
		if index := strings.Index(raw, "/"); index >= 0 {
			raw = raw[index+1:]
		}
		byRaw[raw] = append(byRaw[raw], item.ID)
	}
	if !keys[defaultKey] {
		defaultKey = models[0].ID
	}

	agents, err := st.Agents.List(ctx)
	if err != nil {
		logger.Warn("读取 Agent 列表失败，跳过模型迁移", "error", err)
		return
	}
	for _, item := range agents {
		if keys[item.ModelID] {
			continue
		}
		next := defaultKey
		if candidates := byRaw[item.ModelID]; len(candidates) == 1 {
			next = candidates[0]
		}
		if next == "" || next == item.ModelID {
			continue
		}
		previous := item.ModelID
		item.ModelID = next
		item.UpdatedAt = time.Now().UnixMilli()
		if err := st.Agents.Upsert(ctx, item); err != nil {
			logger.Warn("迁移 Agent 模型失败", "agent", item.ID, "error", err)
			continue
		}
		logger.Info("Agent 模型已迁移", "agent", item.ID, "from", previous, "to", next)
	}
}
