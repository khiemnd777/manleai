-- Opt-in SAMPLE TEST fixture. This file is embedded only by package
-- sampledata; the normal startup migrator cannot discover or apply it.

INSERT INTO salons (
    id,name,phone,address,city,state,zip_code,timezone,owner_user_id,
    primary_language,secondary_language,handoff_phone,ai_enabled,
    public_catalog_enabled,data_classification
)
SELECT
    '10000000-0000-4000-8000-000000000001'::uuid,
    'Lotus Nails Studio',
    '+13125550100',
    '1200 W Sample Ave',
    'Chicago',
    'IL',
    '60601',
    'America/Chicago',
    owner_account.id,
    'en',
    'vi',
    NULL,
    false,
    false,
    'sample_test'
FROM users owner_account
WHERE lower(owner_account.email)='owner@lotusnails.example'
  AND owner_account.status='active'
  AND owner_account.data_classification='sample_test';

INSERT INTO salon_settings (
    salon_id,ai_greeting,ai_voice,ai_tone,booking_mode,
    recording_enabled,sms_confirmation_enabled,sms_reminder_enabled,
    handoff_enabled,consultation_enabled,scheduling_authority,
    customer_sms_enabled
)
VALUES (
    '10000000-0000-4000-8000-000000000001',
    'Thank you for calling Lotus Nails Studio. How can I help today?',
    'professional_female',
    'professional_warm',
    'pending_approval',
    false,
    false,
    false,
    false,
    true,
    'owner_manual',
    false
);

INSERT INTO salon_business_hours (
    id,salon_id,day_of_week,open_time,close_time,is_closed
)
VALUES
    ('16000000-0000-4000-8000-000000000000','10000000-0000-4000-8000-000000000001',0,NULL,NULL,true),
    ('16000000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000001',1,'09:30','19:00',false),
    ('16000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001',2,'09:30','19:00',false),
    ('16000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001',3,'09:30','19:00',false),
    ('16000000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001',4,'09:30','19:00',false),
    ('16000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000001',5,'09:30','19:00',false),
    ('16000000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000001',6,'09:30','19:00',false);

INSERT INTO salon_business_hour_periods (
    id,salon_id,day_of_week,start_local_time,end_local_time,end_at_midnight,
    source,provider,provider_location_id,provider_period_index,last_synced_at
)
VALUES
    ('16100000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000001',1,'09:30','19:00',false,'local_override','','',1,NULL),
    ('16100000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001',2,'09:30','19:00',false,'local_override','','',1,NULL),
    ('16100000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001',3,'09:30','19:00',false,'local_override','','',1,NULL),
    ('16100000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001',4,'09:30','19:00',false,'local_override','','',1,NULL),
    ('16100000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000001',5,'09:30','19:00',false,'local_override','','',1,NULL),
    ('16100000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000001',6,'09:30','19:00',false,'local_override','','',1,NULL);

INSERT INTO service_categories (
    id,salon_id,name,slug,description,status,sort_order,source,reviewed_by,reviewed_at
)
SELECT fixture.id,fixture.salon_id,fixture.name,fixture.slug,fixture.description,'active',fixture.sort_order,'imported',owner_account.id,now()
FROM (VALUES
    ('11000000-0000-4000-8000-000000000001'::uuid,'10000000-0000-4000-8000-000000000001'::uuid,'Manicure','manicure','Hand nail services such as classic and gel manicure appointments.',10),
    ('11000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','Pedicure','pedicure','Foot nail services such as classic and spa pedicure appointments.',20),
    ('11000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001','Acrylic','acrylic','Acrylic full sets and related extension services.',30),
    ('11000000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001','Dip Powder','dip-powder','Dip powder and SNS-style nail services.',40),
    ('11000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000001','Removal','removal','Professional gel polish removal services.',50)
) AS fixture(id,salon_id,name,slug,description,sort_order)
CROSS JOIN users owner_account
WHERE lower(owner_account.email)='owner@lotusnails.example';

INSERT INTO service_category_aliases (
    id,salon_id,category_id,alias,normalized_alias,source,status,confidence
)
VALUES
    ('11100000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000001','classic manicure','classic manicure','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000001','gel manicure','gel manicure','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000001','mani','mani','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000001','manicure','manicure','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000002','classic pedicure','classic pedicure','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000002','pedi','pedi','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000007','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000002','pedicure','pedicure','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000008','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000002','spa pedicure','spa pedicure','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000009','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000003','acrylic','acrylic','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000010','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000003','acrylic full set','acrylic full set','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000011','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000003','full set','full set','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000012','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000004','dip','dip','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000013','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000004','dip powder','dip powder','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000014','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000004','powder manicure','powder manicure','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000015','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000004','sns','sns','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000016','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000005','gel removal','gel removal','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000017','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000005','removal','removal','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000018','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000005','remove','remove','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000019','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000005','soak off','soak off','imported','active',0.940),
    ('11100000-0000-4000-8000-000000000020','10000000-0000-4000-8000-000000000001','11000000-0000-4000-8000-000000000005','take off','take off','imported','active',0.940);

INSERT INTO services (
    id,salon_id,pos_provider,pos_service_id,name,description,ai_description,
    duration_minutes,price_from,price_display,ai_bookable,active,source,sync_status,
    service_category_id,service_category_source,service_category_confidence,
    service_category_reviewed_by,service_category_reviewed_at
)
SELECT fixture.id,fixture.salon_id,'square',NULL,fixture.name,fixture.description,fixture.description,
       fixture.duration_minutes,fixture.price_from,fixture.price_display,true,true,'local','local_only',
       fixture.category_id,'imported',0.940,owner_account.id,now()
FROM (VALUES
    ('12000000-0000-4000-8000-000000000001'::uuid,'10000000-0000-4000-8000-000000000001'::uuid,'Classic Manicure','A simple natural-nail service with shaping, cuticle care, lotion, and regular polish.',30,25.00::numeric,'starting at $25.00','11000000-0000-4000-8000-000000000001'::uuid),
    ('12000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','Gel Manicure','A natural-nail manicure finished with long-wear gel polish.',45,38.00,'starting at $38.00','11000000-0000-4000-8000-000000000001'),
    ('12000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001','Classic Pedicure','Essential foot and toenail care with soak, shaping, cuticle care, massage, and regular polish.',45,40.00,'starting at $40.00','11000000-0000-4000-8000-000000000002'),
    ('12000000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001','Spa Pedicure','An extended pedicure with exfoliation, mask, warm towel, massage, and regular polish.',60,55.00,'starting at $55.00','11000000-0000-4000-8000-000000000002'),
    ('12000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000001','Dip Powder Manicure','A dip powder color service for added strength and longer wear on natural nails.',60,50.00,'starting at $50.00','11000000-0000-4000-8000-000000000004'),
    ('12000000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000001','Acrylic Full Set','A full acrylic extension set with shaping and regular polish for added length and strength.',75,65.00,'starting at $65.00','11000000-0000-4000-8000-000000000003'),
    ('12000000-0000-4000-8000-000000000007','10000000-0000-4000-8000-000000000001','Gel Removal','Professional gel polish removal with light nail cleanup.',20,15.00,'starting at $15.00','11000000-0000-4000-8000-000000000005')
) AS fixture(id,salon_id,name,description,duration_minutes,price_from,price_display,category_id)
CROSS JOIN users owner_account
WHERE lower(owner_account.email)='owner@lotusnails.example';

INSERT INTO service_aliases (
    id,salon_id,service_id,alias,normalized_alias,source,status,confidence
)
VALUES
    ('15000000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000006','acrylic set','acrylic set','import','active',0.940),
    ('15000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000004','deluxe pedicure','deluxe pedicure','import','active',0.940),
    ('15000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000005','dip manicure','dip manicure','import','active',0.940),
    ('15000000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000002','gel mani','gel mani','import','active',0.940),
    ('15000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000007','gel takeoff','gel takeoff','import','active',0.940),
    ('15000000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000001','regular manicure','regular manicure','import','active',0.940),
    ('15000000-0000-4000-8000-000000000007','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000003','regular pedicure','regular pedicure','import','active',0.940);

INSERT INTO service_consultation_profiles (
    id,salon_id,service_id,status,recommended_outcomes,compatible_current_systems,
    length_capabilities,priority_tags,finish_options,maintenance_note,
    owner_approved_summary,updated_by
)
SELECT fixture.id,fixture.salon_id,fixture.service_id,'ready',fixture.outcomes::jsonb,fixture.systems::jsonb,
       fixture.lengths::jsonb,fixture.priorities::jsonb,fixture.finishes::jsonb,
       fixture.maintenance,fixture.summary,owner_account.id
FROM (VALUES
    ('14000000-0000-4000-8000-000000000001'::uuid,'10000000-0000-4000-8000-000000000001'::uuid,'12000000-0000-4000-8000-000000000001'::uuid,'["maintain","shorten","color_refresh"]','["natural","regular_polish"]','["keep","shorten"]','["lower_cost","shorter_visit"]','["natural","regular_polish","glossy"]','Regular polish is easy to remove at home, but it may chip sooner than gel or dip.','A simple natural-nail service with shaping, cuticle care, lotion, and regular polish.'),
    ('14000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000002','["maintain","shorten","color_refresh"]','["natural","regular_polish","gel"]','["keep","shorten"]','["durability","lower_maintenance"]','["gel_polish","glossy"]','Refresh or professionally remove the gel as nail growth becomes visible; do not peel it off.','A natural-nail manicure finished with long-wear gel polish for a durable, glossy result.'),
    ('14000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000003','["maintain","shorten","color_refresh"]','["natural","regular_polish"]','["keep","shorten"]','["lower_cost","shorter_visit"]','["natural","regular_polish","glossy"]','Regular polish may chip sooner than gel; allow it to dry fully and refresh as needed.','Essential foot and toenail care with soak, shaping, cuticle care, massage, and regular polish.'),
    ('14000000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000004','["maintain","shorten","color_refresh"]','["natural","regular_polish"]','["keep","shorten"]','[]','["natural","regular_polish","glossy"]','Regular polish may chip sooner than gel; allow it to dry fully and refresh as needed.','An extended pedicure with exfoliation, mask, warm towel, massage, and regular polish.'),
    ('14000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000005','["maintain","shorten","add_strength","color_refresh"]','["natural","regular_polish","dip"]','["keep","shorten"]','["durability","lower_maintenance"]','["glossy"]','Dip powder grows out with the natural nail and should be soaked off or refreshed instead of peeled.','A dip powder color service for added strength and longer wear on natural nails.'),
    ('14000000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000006','["add_length","add_strength"]','["natural","regular_polish"]','["add_length"]','["durability"]','["regular_polish","glossy"]','As the natural nail grows, acrylic extensions commonly need a salon fill or professional removal.','A full acrylic extension set with shaping and regular polish for added length and strength.'),
    ('14000000-0000-4000-8000-000000000007','10000000-0000-4000-8000-000000000001','12000000-0000-4000-8000-000000000007','["removal"]','["gel"]','["keep"]','["lower_cost","shorter_visit"]','["natural"]','Professional soak-off helps avoid damage from peeling.','Professional gel polish removal with light nail cleanup.')
) AS fixture(id,salon_id,service_id,outcomes,systems,lengths,priorities,finishes,maintenance,summary)
CROSS JOIN users owner_account
WHERE lower(owner_account.email)='owner@lotusnails.example';

INSERT INTO staff (
    id,salon_id,pos_provider,pos_staff_id,name,phone,email,ai_bookable,active,source,sync_status
)
VALUES
    ('13000000-0000-4000-8000-000000000001','10000000-0000-4000-8000-000000000001','square',NULL,'Linh Nguyen',NULL,NULL,true,true,'local','local_only'),
    ('13000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','square',NULL,'Mai Tran',NULL,NULL,true,true,'local','local_only'),
    ('13000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001','square',NULL,'Vy Pham',NULL,NULL,true,true,'local','local_only'),
    ('13000000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000001','square',NULL,'Hannah Le',NULL,NULL,true,true,'local','local_only');

INSERT INTO pos_entity_links (
    salon_id,entity_type,entity_id,provider,provider_entity_id,sync_status
)
SELECT service.salon_id,'service',service.id,'square',NULL,'local_only'
FROM services service
WHERE service.salon_id='10000000-0000-4000-8000-000000000001'
UNION ALL
SELECT member.salon_id,'staff',member.id,'square',NULL,'local_only'
FROM staff member
WHERE member.salon_id='10000000-0000-4000-8000-000000000001';

INSERT INTO manleai_calendar_configs (
    salon_id,version,slot_step_minutes,minimum_booking_notice_minutes,
    booking_horizon_days,reschedule_cutoff_minutes,cancellation_cutoff_minutes,
    max_party_size,default_buffer_before_minutes,default_buffer_after_minutes
)
VALUES (
    '10000000-0000-4000-8000-000000000001',1,15,120,90,1440,1440,6,0,0
);

INSERT INTO manleai_calendar_service_staff (salon_id,service_id,staff_id)
SELECT service.salon_id,service.id,member.id
FROM services service
CROSS JOIN staff member
WHERE service.salon_id='10000000-0000-4000-8000-000000000001'
  AND member.salon_id=service.salon_id;
