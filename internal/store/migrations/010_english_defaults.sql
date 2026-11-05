ALTER TABLE sessions ALTER COLUMN title SET DEFAULT 'New chat';

UPDATE sessions SET title='New chat' WHERE title='新对话';
UPDATE sessions SET title='Image chat' WHERE title='图片对话';

UPDATE outbox_messages
SET error='Chat preset is locked. This message used a different preset; start a new chat to continue.'
WHERE error='会话工具模式已固定；此消息使用了不同模式，请新建会话后发送';

UPDATE outbox_messages
SET error='The server restarted before this turn was delivered. Please retry.'
WHERE error='后端重启，本轮尚未送达，请重发';
