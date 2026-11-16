WITH ranked AS (
  SELECT
    id,
    ROW_NUMBER() OVER (
      PARTITION BY lower(title)
      ORDER BY
        CASE
          WHEN magnet LIKE 'magnet:%' AND COALESCE(magnet_hash, '') <> '' THEN 0
          WHEN magnet LIKE 'magnet:%' THEN 1
          ELSE 2
        END,
        added_at DESC,
        id DESC
    ) AS rn
  FROM archive
  WHERE trim(COALESCE(title, '')) <> ''
)
DELETE FROM archive
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);
