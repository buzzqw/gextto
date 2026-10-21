-- gextto:title=Duplicati per titolo e fonte
-- gextto:description=Mostra e cancella le release con solo un link .torrent ripetute per titolo e fonte, tenendo la riga più recente.
-- gextto:category=Pulizie mirate
-- gextto:order=40
WITH ranked AS (
  SELECT
    id,
    title,
    source,
    magnet,
    added_at,
    COUNT(*) OVER (PARTITION BY source, lower(title)) AS copies,
    ROW_NUMBER() OVER (PARTITION BY source, lower(title) ORDER BY added_at DESC, id DESC) AS rn
  FROM archive
  WHERE magnet LIKE 'http%'
)
SELECT
  id,
  added_at AS "Aggiunta il",
  title AS "Titolo",
  COALESCE(NULLIF(source, ''), '(senza fonte)') AS "Fonte",
  copies AS "Copie",
  magnet AS "Link"
FROM ranked
WHERE rn > 1
ORDER BY copies DESC, lower(title) ASC, added_at DESC, id DESC;
