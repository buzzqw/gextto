WITH norm AS (
  SELECT
    id,
    title,
    added_at,
    lower(title) AS raw,
    ' ' || trim(
      replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(
        lower(title),
        '.', ' '), '_', ' '), '-', ' '), '[', ' '), ']', ' '), '(', ' '), ')', ' '),
        '{', ' '), '}', ' '), '+', ' '), '/', ' '), '|', ' ')
    ) || ' ' AS t
  FROM archive
),
flagged AS (
  SELECT
    *,
    CASE WHEN
      EXISTS (SELECT 1 FROM json_each(:tokens) m WHERE t LIKE '% ' || lower(m.value) || ' %')
      OR EXISTS (SELECT 1 FROM json_each(:raw) m WHERE raw LIKE '%' || lower(m.value) || '%')
    THEN 1 ELSE 0 END AS is_language
  FROM norm
)
DELETE FROM archive
WHERE id IN (
  SELECT id
  FROM flagged
  WHERE is_language = 1 AND datetime(added_at) < datetime('now', '-6 months')
);
