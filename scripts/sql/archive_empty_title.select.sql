-- gextto:title=Titoli vuoti
-- gextto:description=Mostra e cancella le righe dell'archivio senza titolo.
-- gextto:category=Pulizie mirate
-- gextto:order=50
SELECT id, added_at AS "Aggiunta il", title AS "Titolo", source AS "Fonte"
FROM archive
WHERE trim(COALESCE(title, '')) = ''
ORDER BY datetime(added_at) ASC, id ASC;
