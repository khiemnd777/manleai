-- Pending-approval AI conversations store the selected executable authority
-- on the owner-review request without changing the request root authority.
-- Legacy owner_manual book requests keep their NULL target exactly so their
-- immutable fingerprints and exact replay remain valid.
ALTER TABLE scheduling_requests
    DROP CONSTRAINT scheduling_requests_operation_target_check;

ALTER TABLE scheduling_requests
    ADD CONSTRAINT scheduling_requests_operation_target_check
        CHECK (
            (
                operation_type = 'book'
                AND target_appointment_id IS NULL
                AND target_description IS NULL
            )
            OR (
                operation_type IN ('reschedule', 'cancel')
                AND (target_appointment_id IS NOT NULL OR target_description IS NOT NULL)
                AND (target_appointment_id IS NULL OR target_scheduling_authority IS NOT NULL)
            )
        );
