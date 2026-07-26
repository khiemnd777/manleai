-- Phase 1 SaaS hardening: remove provider-controlled diagnostics that may have
-- been persisted before provider errors were normalized at the runtime edge.
CREATE FUNCTION phase1_safe_pos_error_message(input_code TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE btrim(COALESCE(input_code, ''))
        WHEN 'POS_TOKEN_EXPIRED' THEN 'The POS access token is expired or unauthorized. Reconnect the POS account.'
        WHEN 'POS_PERMISSION_DENIED' THEN 'The POS provider rejected the operation because required permissions are missing.'
        WHEN 'POS_LOCATION_NOT_SELECTED' THEN 'A POS location has not been selected for this salon.'
        WHEN 'POS_AVAILABILITY_FAILED' THEN 'The POS provider could not check appointment availability.'
        WHEN 'POS_BOOKING_FAILED' THEN 'The POS provider could not complete the appointment operation.'
        WHEN 'POS_BOOKING_CONFLICT' THEN 'The POS provider rejected the operation because the appointment conflicts with current provider state.'
        WHEN 'POS_RATE_LIMITED' THEN 'The POS provider rate-limited the operation. Try again later.'
        WHEN 'POS_TIMEOUT' THEN 'The POS provider operation timed out.'
        WHEN 'POS_WRITE_UNSUPPORTED' THEN 'The connected POS integration does not support this write operation.'
        WHEN 'POS_CUSTOMER_CREATE_FAILED' THEN 'The POS provider could not create the customer record.'
        ELSE 'The POS provider operation failed.'
    END
$$;

UPDATE pos_errors
SET error_message = phase1_safe_pos_error_message(error_code),
    payload = NULL;

UPDATE booking_attempts
SET error_message = phase1_safe_pos_error_message(error_code)
WHERE error_message IS NOT NULL;

UPDATE pos_connections
SET error_message = CASE
        WHEN status = 'expired_token' THEN phase1_safe_pos_error_message('POS_TOKEN_EXPIRED')
        ELSE phase1_safe_pos_error_message(NULL)
    END
WHERE error_message IS NOT NULL;

UPDATE pos_sync_logs
SET message = phase1_safe_pos_error_message(NULL)
WHERE status = 'failed'
  AND message IS NOT NULL;

UPDATE pos_sync_jobs
SET last_error = phase1_safe_pos_error_message(NULL)
WHERE last_error IS NOT NULL;

UPDATE pos_entity_links
SET last_error = phase1_safe_pos_error_message(NULL)
WHERE sync_status = 'sync_failed'
  AND last_error IS NOT NULL;

UPDATE services
SET sync_error = phase1_safe_pos_error_message(NULL)
WHERE sync_status = 'sync_failed'
  AND sync_error IS NOT NULL;

UPDATE staff
SET sync_error = phase1_safe_pos_error_message(NULL)
WHERE sync_status = 'sync_failed'
  AND sync_error IS NOT NULL;

UPDATE customers
SET sync_error = phase1_safe_pos_error_message(NULL)
WHERE sync_status = 'sync_failed'
  AND sync_error IS NOT NULL;

UPDATE appointments
SET pos_sync_error = phase1_safe_pos_error_message(NULL)
WHERE pos_sync_error IS NOT NULL;

-- Twilio stream-status rows previously copied the complete callback form. Keep
-- only the fields needed to correlate lifecycle state and terminal failures.
UPDATE voice_webhook_events
SET payload = jsonb_strip_nulls(jsonb_build_object(
        'stage', 'twilio_stream_status',
        'stream_sid', CASE
            WHEN COALESCE(payload->>'stream_sid', payload->>'StreamSid', '') ~ '^[A-Za-z0-9._:-]{1,128}$'
                THEN COALESCE(payload->>'stream_sid', payload->>'StreamSid')
            ELSE NULL
        END,
        'stream_event', CASE
            WHEN COALESCE(payload->>'stream_event', payload->>'StreamEvent', '') ~ '^[A-Za-z0-9._:-]{1,128}$'
                THEN lower(COALESCE(payload->>'stream_event', payload->>'StreamEvent'))
            ELSE NULL
        END,
        'terminal', CASE
            WHEN lower(COALESCE(payload->>'stream_event', payload->>'StreamEvent', '')) = 'stream-error' THEN 'true'
            ELSE NULL
        END,
        'error_code', CASE
            WHEN lower(COALESCE(payload->>'stream_event', payload->>'StreamEvent', '')) = 'stream-error' THEN 'TWILIO_STREAM_ERROR'
            ELSE NULL
        END
    ))
WHERE payload->>'stage' = 'twilio_stream_status';

-- Older realtime failure rows could contain provider response text in `error`.
UPDATE voice_webhook_events
SET payload = (payload - 'error' - 'StreamError')
    || jsonb_build_object('error', 'Realtime operation failed.')
WHERE event_type = 'realtime_failed'
  AND (payload ? 'error' OR payload ? 'StreamError');

DROP FUNCTION phase1_safe_pos_error_message(TEXT);
