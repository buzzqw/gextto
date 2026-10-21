-- gextto:title=Distribuzione per età
-- gextto:description=Conteggio delle release per fascia di età.
-- gextto:category=Diagnostica
-- gextto:order=60
SELECT
  CASE
    WHEN datetime(added_at) >= datetime('now', '-30 days') THEN '0-30 giorni'
    WHEN datetime(added_at) >= datetime('now', '-90 days') THEN '31-90 giorni'
    WHEN datetime(added_at) >= datetime('now', '-180 days') THEN '91-180 giorni'
    WHEN datetime(added_at) >= datetime('now', '-365 days') THEN '181-365 giorni'
    ELSE 'oltre 1 anno'
  END AS "Fascia",
  COUNT(*) AS "Voci",
  MIN(added_at) AS "Più vecchia",
  MAX(added_at) AS "Più recente"
FROM archive
GROUP BY "Fascia"
ORDER BY MIN(datetime(added_at)) ASC;
