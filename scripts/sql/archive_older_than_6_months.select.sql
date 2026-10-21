-- gextto:title=Release più vecchie di 6 mesi
-- gextto:description=Mostra e cancella le release entrate in archivio da più di 6 mesi.
-- gextto:category=Età
-- gextto:order=10
SELECT id, added_at AS "Aggiunta il", title AS "Titolo", source AS "Fonte"
FROM archive
WHERE datetime(added_at) < datetime('now', '-6 months')
ORDER BY datetime(added_at) ASC, id ASC;
