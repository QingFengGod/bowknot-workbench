// Package store 是所有结构化数据的唯一入口。
package store

// 注册纯 Go SQLite 驱动，后续 Open 使用 "sqlite" 作为驱动名。
import _ "modernc.org/sqlite"
