-- gextto:title=Stato dell'archivio
-- gextto:description=Controlli rapidi su titoli, magnet, hash e indice full-text.
-- gextto:category=Diagnostica
-- gextto:order=80
SELECT
  (SELECT COUNT(*) FROM archive) AS "Release totali",
  (SELECT COUNT(*) FROM archive WHERE trim(COALESCE(title, '')) = '') AS "Titoli vuoti",
  (SELECT COUNT(*) FROM archive WHERE trim(COALESCE(magnet, '')) = '') AS "Magnet vuoti",
  (SELECT COUNT(*) FROM archive WHERE magnet NOT LIKE 'magnet:%' AND magnet NOT LIKE 'http://%' AND magnet NOT LIKE 'https://%') AS "Magnet non validi",
  (SELECT COUNT(*) FROM archive WHERE COALESCE(magnet_hash, '') = '') AS "Hash mancanti",
  (SELECT COUNT(*) FROM archive_fts) AS "Righe FTS",
  (SELECT COUNT(*) FROM archive_fts) - (SELECT COUNT(*) FROM archive) AS "Differenza FTS",
  (SELECT COUNT(*) FROM archive_fts f WHERE NOT EXISTS (SELECT 1 FROM archive a WHERE a.id = f.rowid)) AS "Orfani FTS";
