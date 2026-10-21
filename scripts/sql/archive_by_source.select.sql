-- gextto:title=Release per fonte
-- gextto:description=Conteggio delle release per fonte, con la più vecchia e la più recente.
-- gextto:category=Diagnostica
-- gextto:order=70
SELECT
  COALESCE(NULLIF(source, ''), '(senza fonte)') AS "Fonte",
  COUNT(*) AS "Voci",
  MIN(added_at) AS "Più vecchia",
  MAX(added_at) AS "Più recente"
FROM archive
GROUP BY COALESCE(NULLIF(source, ''), '(senza fonte)')
ORDER BY COUNT(*) DESC, "Fonte" ASC;
