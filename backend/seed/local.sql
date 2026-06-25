-- Local-only seed. Do not run this against production.
-- Login: owner@lotusnails.example / password123
INSERT INTO users (email, password_hash, full_name, phone, status)
VALUES (
    'owner@lotusnails.example',
    crypt('password123', gen_salt('bf', 10)),
    'Linh Nguyen',
    '+13125550100',
    'active'
)
ON CONFLICT (email) DO UPDATE
SET password_hash = EXCLUDED.password_hash,
    full_name = EXCLUDED.full_name,
    phone = EXCLUDED.phone,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users u
JOIN roles r ON r.name = 'salon_owner'
WHERE u.email = 'owner@lotusnails.example'
ON CONFLICT DO NOTHING;

INSERT INTO salons (name, phone, address, city, state, zip_code, timezone, owner_user_id, handoff_phone, ai_enabled)
SELECT 'Lotus Nails Studio', '+16292536211', '1200 W Sample Ave', 'Chicago', 'IL', '60601', 'America/Chicago', u.id, '+13125550102', false
FROM users u
WHERE u.email = 'owner@lotusnails.example'
ON CONFLICT DO NOTHING;

INSERT INTO salon_settings (salon_id)
SELECT s.id
FROM salons s
WHERE s.name = 'Lotus Nails Studio'
ON CONFLICT (salon_id) DO NOTHING;

INSERT INTO salon_business_hours (salon_id, day_of_week, open_time, close_time, is_closed)
SELECT s.id, d.day_of_week, '09:30'::time, '19:00'::time, d.day_of_week = 0
FROM salons s
CROSS JOIN generate_series(0, 6) AS d(day_of_week)
WHERE s.name = 'Lotus Nails Studio'
ON CONFLICT (salon_id, day_of_week) DO NOTHING;

INSERT INTO salon_business_hour_periods (
    salon_id,
    day_of_week,
    start_local_time,
    end_local_time,
    source,
    provider,
    provider_location_id,
    provider_period_index
)
SELECT s.id, d.day_of_week, '09:30'::time, '19:00'::time, 'local_migrated', '', '', 0
FROM salons s
CROSS JOIN generate_series(1, 6) AS d(day_of_week)
WHERE s.name = 'Lotus Nails Studio'
ON CONFLICT (salon_id, provider, provider_location_id, day_of_week, provider_period_index) DO NOTHING;
