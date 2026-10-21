WITH ranked AS (
  SELECT
    id,
    ROW_NUMBER() OVER (PARTITION BY source, lower(title) ORDER BY added_at DESC, id DESC) AS rn
  FROM archive
  WHERE magnet LIKE 'http%'
)
DELETE FROM archive
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);
