DELETE FROM archive
WHERE datetime(added_at) < datetime('now', '-1 year');
