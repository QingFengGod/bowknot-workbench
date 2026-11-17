-- 历史上创建 Agent 允许人设为空，运行时只能退回到一条通用提示词，
-- 看起来像所有 Agent 共用同一个身份。这里按名称与描述回填最小可用的独立人设，
-- 与 app 层 defaultAgentPersona 的文案保持一致，用户仍可在界面上修改。
UPDATE agent
SET persona = '你是「' || name || '」，' ||
    CASE
        WHEN TRIM(description) = '' OR description = '暂无描述'
            THEN '请用简洁的中文回复用户。'
        ELSE description || '。请围绕这个职责用简洁的中文回复。'
    END
WHERE TRIM(persona) = '';
