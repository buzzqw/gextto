DELETE FROM archive
WHERE datetime(added_at) < datetime('now', '-6 months');
