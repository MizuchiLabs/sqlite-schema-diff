CREATE TRIGGER update_empty_posts AFTER INSERT ON posts BEGIN
UPDATE posts
SET
  content = 'Empty'
WHERE
  id = NEW.id
  AND content IS NULL;

END;
