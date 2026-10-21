DELETE FROM archive
WHERE trim(COALESCE(title, '')) = '';
