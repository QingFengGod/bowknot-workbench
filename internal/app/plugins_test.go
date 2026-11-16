package app

import (
	"context"
	"testing"

	"bowknot-workbench/internal/contract"
	pkgs "bowknot-workbench/internal/infra/package"
)

func TestPluginServiceInstallGrantAndUninstall(t *testing.T) {
	env := newCatalogEnv(t)
	svc := NewPluginService(env.store, env.installer, env.tools, testLogger())

	tarPath := writePackage(t, t.TempDir(), "web-search", "网页搜索", "1.2.0",
		pkgs.ManifestTool{Name: "searchWeb", Description: "搜索网页"})

	if err := svc.Install(contract.InstallSource{Type: "local", Value: tarPath}); err != nil {
		t.Fatalf("install: %v", err)
	}
	plugins := svc.Plugins()
	if len(plugins) != 1 {
		t.Fatalf("plugin count = %d, want 1", len(plugins))
	}
	plugin := plugins[0]
	if plugin.Name != "网页搜索" || plugin.IconText != "网" || plugin.Version != "1.2.0" {
		t.Fatalf("plugin = %+v", plugin)
	}
	if len(plugin.Tools) != 1 || plugin.Tools[0].Name != "searchWeb" {
		t.Fatalf("plugin tools = %+v", plugin.Tools)
	}
	if !hasRegistryTool(env.tools, "plugin:web-search/searchWeb") {
		t.Fatal("安装后插件工具未同步到注册表")
	}

	// 主 Agent 始终可选，用户 Agent 来自 store。
	foundMain := false
	for _, agent := range svc.Agents() {
		if agent.IsMain && agent.ID == "main-agent" {
			foundMain = true
		}
	}
	if !foundMain {
		t.Fatal("插件授权列表缺少主 Agent")
	}

	if err := svc.SetAuthorizedAgents("web-search", []string{"main-agent", "不存在的 Agent"}); err != nil {
		t.Fatalf("set authorized agents: %v", err)
	}
	plugins = svc.Plugins()
	if len(plugins[0].AgentIDs) != 1 || plugins[0].AgentIDs[0] != "main-agent" {
		t.Fatalf("授权列表 = %+v", plugins[0].AgentIDs)
	}
	item, err := env.store.Plugins.Get(context.Background(), "web-search")
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}
	if len(item.AgentIDs) != 1 || item.AgentIDs[0] != "main-agent" {
		t.Fatalf("授权关系未落库：%+v", item.AgentIDs)
	}

	if err := svc.Uninstall("web-search"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(svc.Plugins()) != 0 {
		t.Fatal("卸载后插件记录应被删除")
	}
	if hasRegistryTool(env.tools, "plugin:web-search/searchWeb") {
		t.Fatal("卸载后插件工具应从注册表移除")
	}
}
