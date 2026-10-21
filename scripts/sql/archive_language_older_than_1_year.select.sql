-- gextto:title=Release nella lingua selezionata più vecchie di 1 anno
-- gextto:description=Mostra e cancella le release nella lingua scelta nel pannello, entrate in archivio da più di 1 anno. La lingua è dedotta dal titolo.
-- gextto:category=Lingua
-- gextto:order=40
-- gextto:param=language
WITH norm AS (
  SELECT
    id,
    title,
    source,
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
SELECT id, added_at AS "Aggiunta il", title AS "Titolo", source AS "Fonte"
FROM flagged
WHERE is_language = 1 AND datetime(added_at) < datetime('now', '-1 year')
ORDER BY datetime(added_at) ASC, id ASC;
