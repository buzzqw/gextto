-- gextto:title=Release più vecchie di 1 anno
-- gextto:description=Mostra e cancella le release entrate in archivio da più di 1 anno.
-- gextto:category=Età
-- gextto:order=30
SELECT id, added_at AS "Aggiunta il", title AS "Titolo", source AS "Fonte"
FROM archive
WHERE datetime(added_at) < datetime('now', '-1 year')
ORDER BY datetime(added_at) ASC, id ASC;
