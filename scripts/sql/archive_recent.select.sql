-- gextto:title=Release recenti
-- gextto:description=Mostra le release entrate in archivio di recente, dalla più nuova.
-- gextto:category=Diagnostica
-- gextto:order=5
SELECT
  id,
  added_at AS "Aggiunta il",
  title AS "Titolo",
  COALESCE(NULLIF(source, ''), '(senza fonte)') AS "Fonte",
  magnet AS "Link"
FROM archive
ORDER BY datetime(added_at) DESC, id DESC;
