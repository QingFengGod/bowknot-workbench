-- 记忆日期此前由抽取模型自由生成，可能偏离真实写入时间；
-- 统一回填为写入时间，并清空可再生的日摘要，避免按错误日期聚合。
UPDATE memory SET occurred_at = created_at, day_key = date(created_at / 1000, 'unixepoch', 'localtime');

DELETE FROM memory_digest_item;
DELETE FROM memory_digest;
