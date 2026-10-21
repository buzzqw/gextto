-- gextto:title=Titoli duplicati
-- gextto:description=Titoli ripetuti con il numero di copie.
-- gextto:category=Diagnostica
-- gextto:order=90
SELECT
  lower(title) AS "Titolo",
  COUNT(*) AS "Copie",
  MIN(added_at) AS "Più vecchia",
  MAX(added_at) AS "Più recente"
FROM archive
WHERE trim(COALESCE(title, '')) <> ''
GROUP BY lower(title)
HAVING COUNT(*) > 1
ORDER BY COUNT(*) DESC, lower(title) ASC;
