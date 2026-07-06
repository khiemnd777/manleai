ALTER TABLE salon_settings
    ALTER COLUMN ai_greeting SET DEFAULT 'Thank you for calling. How can I help today?',
    ALTER COLUMN recording_consent_message SET DEFAULT 'This call may be recorded to help us manage appointments and improve service.';

UPDATE salon_settings
SET ai_greeting = 'Thank you for calling. How can I help today?'
WHERE ai_greeting = 'Thank you for calling. This call may be recorded to help us manage appointments and improve service.';

UPDATE salon_settings
SET recording_consent_message = 'This call may be recorded to help us manage appointments and improve service.'
WHERE recording_consent_message = 'Thank you for calling. This call may be recorded to help us manage appointments and improve service.';
