ALTER TABLE call_sessions
    ADD COLUMN requested_date DATE;

UPDATE call_sessions cs
SET requested_date = (cs.requested_start_time AT TIME ZONE COALESCE(NULLIF(s.timezone, ''), 'UTC'))::date
FROM salons s
WHERE cs.salon_id = s.id
  AND cs.requested_start_time IS NOT NULL
  AND cs.requested_date IS NULL;

CREATE INDEX idx_call_sessions_requested_date ON call_sessions(salon_id, requested_date)
WHERE requested_date IS NOT NULL;
