CREATE INDEX IF NOT EXISTS idx_owner_notifications_salon_created
ON owner_notifications(salon_id, created_at ASC, id ASC);
