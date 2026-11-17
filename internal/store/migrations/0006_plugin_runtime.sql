-- 插件运行方式与权限声明：runtime / permissions 存 JSON，
-- 工具参数与超时随工具记录保存，应用工具同步补齐。
ALTER TABLE plugin ADD COLUMN runtime TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plugin ADD COLUMN permissions TEXT NOT NULL DEFAULT '{}';
ALTER TABLE plugin_tool ADD COLUMN parameters TEXT NOT NULL DEFAULT '';
ALTER TABLE plugin_tool ADD COLUMN timeout_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE app_tool ADD COLUMN parameters TEXT NOT NULL DEFAULT '';
ALTER TABLE app_tool ADD COLUMN timeout_ms INTEGER NOT NULL DEFAULT 0;
