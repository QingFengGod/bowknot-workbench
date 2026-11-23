package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bowknot-workbench/internal/contract"
	"bowknot-workbench/internal/domain"
	"bowknot-workbench/internal/infra/tool"
)

func callFS(t *testing.T, fs *FSService, id, args string) tool.Result {
	t.Helper()
	result, _, err := fs.Execute(context.Background(), tool.Call{
		Tool:      tool.ToolSpec{ID: id, Name: id},
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("%s(%s) error = %v", id, args, err)
	}
	return result
}

func TestFSServiceWorksInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	fs := NewFSService(root)

	result, path, err := fs.Execute(context.Background(), tool.Call{
		Tool:      tool.ToolSpec{ID: tool.WriteFileToolID},
		Arguments: `{"path":"项目A/说明.md","content":"第一行\n第二行"}`,
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	want := filepath.Join(root, "项目A", "说明.md")
	if path != want {
		t.Fatalf("写入路径 = %q, want %q", path, want)
	}
	if !strings.Contains(result.Content, "已写入") {
		t.Fatalf("写入结果 = %q", result.Content)
	}
	body, err := os.ReadFile(want)
	if err != nil || string(body) != "第一行\n第二行" {
		t.Fatalf("文件内容 = %q, err = %v", string(body), err)
	}

	read := callFS(t, fs, tool.ReadFileToolID, `{"path":"项目A/说明.md"}`)
	if read.Content != "第一行\n第二行" {
		t.Fatalf("读取结果 = %q", read.Content)
	}

	edit := callFS(t, fs, tool.EditFileToolID, `{"path":"项目A/说明.md","oldText":"第二行","newText":"改过的行"}`)
	if !strings.Contains(edit.Content, "已编辑") {
		t.Fatalf("编辑结果 = %q", edit.Content)
	}
	body, _ = os.ReadFile(want)
	if !strings.Contains(string(body), "改过的行") {
		t.Fatalf("编辑后内容 = %q", string(body))
	}

	// 重复匹配且未开启 replaceAll 时必须拒绝，避免误改。
	if _, _, err := fs.Execute(context.Background(), tool.Call{
		Tool:      tool.ToolSpec{ID: tool.EditFileToolID},
		Arguments: `{"path":"项目A/说明.md","oldText":"行","newText":"x"}`,
	}); err == nil || !strings.Contains(err.Error(), "出现") {
		t.Fatalf("重复匹配 error = %v", err)
	}

	callFS(t, fs, tool.MakeDirToolID, `{"path":"项目A/子目录"}`)
	list := callFS(t, fs, tool.ListDirToolID, `{"path":"","depth":2}`)
	for _, want := range []string{"[目录] 项目A", "[目录] 子目录", "[文件] 说明.md"} {
		if !strings.Contains(list.Content, want) {
			t.Fatalf("目录列表缺少 %q：\n%s", want, list.Content)
		}
	}

	glob := callFS(t, fs, tool.GlobToolID, `{"pattern":"**/*.md"}`)
	if !strings.Contains(glob.Content, "项目A/说明.md") {
		t.Fatalf("glob 结果 = %q", glob.Content)
	}

	search := callFS(t, fs, tool.SearchFilesToolID, `{"pattern":"改过的"}`)
	if !strings.Contains(search.Content, "项目A/说明.md:2") {
		t.Fatalf("搜索结果 = %q", search.Content)
	}

	del := callFS(t, fs, tool.DeleteFileToolID, `{"path":"项目A/说明.md"}`)
	if !strings.Contains(del.Content, "已删除") {
		t.Fatalf("删除结果 = %q", del.Content)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("删除后文件仍存在：%v", err)
	}
}

func TestFSServiceRejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	fs := NewFSService(root)
	ctx := context.Background()

	cases := []string{
		`{"path":"../escape.txt","content":"x"}`,
		`{"path":"` + strings.ReplaceAll(filepath.Join(t.TempDir(), "outside.txt"), `\`, `\\`) + `","content":"x"}`,
	}
	for _, args := range cases {
		if _, _, err := fs.Execute(ctx, tool.Call{
			Tool:      tool.ToolSpec{ID: tool.WriteFileToolID},
			Arguments: args,
		}); err == nil || !strings.Contains(err.Error(), "超出工作区") {
			t.Fatalf("越界写入 %s error = %v", args, err)
		}
	}
}

func TestToolExecutorRegistersWrittenFile(t *testing.T) {
	svc, st, _ := newArtifactStack(t)
	ctx := context.Background()
	if err := st.Conversations.Create(ctx, domain.Conversation{
		ID: "conv-1", Kind: domain.KindAssistant, Title: "主助手", CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}

	workspace := t.TempDir()
	exec := NewToolExecutor(st, nil)
	exec.SetArtifacts(svc)
	exec.SetFS(NewFSService(workspace))

	result, err := exec.Execute(ctx, tool.Call{
		Tool:           tool.ToolSpec{ID: tool.WriteFileToolID},
		ConversationID: "conv-1",
		Arguments:      `{"path":"docs/报告.md","content":"# 报告"}`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "已加入文件库") {
		t.Fatalf("工具结果 = %q", result.Content)
	}

	items := svc.Artifacts(contract.ArtifactFilter{Limit: 10})
	if len(items) != 1 {
		t.Fatalf("文件库条目 = %+v", items)
	}
	want := filepath.Join(workspace, "docs", "报告.md")
	if !items[0].External || items[0].Path != want {
		t.Fatalf("登记详情 = %+v, want path %q", items[0], want)
	}
}
