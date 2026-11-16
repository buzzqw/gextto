-- gextto:title=Titoli duplicati
-- gextto:description=Mostra e cancella le copie con lo stesso titolo: resta la riga con magnet, altrimenti la più recente con un link .torrent.
-- gextto:category=Pulizie mirate
-- gextto:order=60
WITH ranked AS (
  SELECT
    id,
    title,
    source,
    magnet,
    added_at,
    COUNT(*) OVER (PARTITION BY lower(title)) AS copies,
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
SELECT
  id,
  added_at AS "Aggiunta il",
  title AS "Titolo",
  COALESCE(NULLIF(source, ''), '(senza fonte)') AS "Fonte",
  CASE WHEN magnet LIKE 'magnet:%' THEN 'magnet' ELSE '.torrent' END AS "Tipo",
  copies AS "Copie",
  magnet AS "Link"
FROM ranked
WHERE rn > 1
ORDER BY copies DESC, lower(title) ASC, added_at DESC, id DESC;
