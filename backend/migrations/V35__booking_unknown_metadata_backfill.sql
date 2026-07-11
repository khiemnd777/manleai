UPDATE booking_attempts
SET provider_outcome = 'unknown',
    retry_policy = 'blocked',
    reconciliation_status = 'required'
WHERE status = 'fallback_pending'
  AND error_message IN (
      'pos booking id was not returned',
      'pos booking version was not returned'
  );
