-- Normalize interactive actor authorization and allow immutable authority
-- switch audit rows to store the actual authorized Platform actor.

CREATE OR REPLACE FUNCTION enforce_scheduling_authority_switch_run_actor()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.salons salon
        WHERE salon.id = NEW.salon_id
          AND (
              salon.owner_user_id = NEW.actor_user_id
              OR public.app_actor_feature_access(NEW.actor_user_id, NEW.salon_id, 'technical.write')
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling authority switch run actor is not authorized',
            CONSTRAINT = 'scheduling_authority_switch_runs_actor_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_scheduling_authority_switch_event_actor()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.salons salon
        JOIN public.scheduling_authority_switch_runs switch_run
          ON switch_run.id = NEW.switch_run_id
         AND switch_run.salon_id = NEW.salon_id
        WHERE salon.id = NEW.salon_id
          AND (
              salon.owner_user_id = NEW.actor_user_id
              OR public.app_actor_feature_access(NEW.actor_user_id, NEW.salon_id, 'technical.write')
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling authority switch event actor is not authorized',
            CONSTRAINT = 'scheduling_authority_switch_events_actor_guard';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM public.scheduling_authority_switch_runs switch_run
        WHERE switch_run.id = NEW.switch_run_id
          AND switch_run.salon_id = NEW.salon_id
          AND (
              (NEW.event_type = 'preview' AND switch_run.status IN ('preview_ready', 'preview_blocked'))
              OR (NEW.event_type = 'commit' AND switch_run.status = 'committed')
              OR (NEW.event_type = 'fail' AND switch_run.status = 'failed')
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'scheduling authority switch event must match the durable run state',
            CONSTRAINT = 'scheduling_authority_switch_events_state_guard';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS scheduling_authority_switch_runs_owner_actor_guard
    ON public.scheduling_authority_switch_runs;
DROP TRIGGER IF EXISTS scheduling_authority_switch_runs_actor_guard
    ON public.scheduling_authority_switch_runs;
CREATE TRIGGER scheduling_authority_switch_runs_actor_guard
BEFORE INSERT OR UPDATE OF salon_id, actor_user_id
ON public.scheduling_authority_switch_runs
FOR EACH ROW EXECUTE FUNCTION enforce_scheduling_authority_switch_run_actor();

DROP TRIGGER IF EXISTS scheduling_authority_switch_events_owner_actor_guard
    ON public.scheduling_authority_switch_events;
DROP TRIGGER IF EXISTS scheduling_authority_switch_events_actor_guard
    ON public.scheduling_authority_switch_events;
CREATE TRIGGER scheduling_authority_switch_events_actor_guard
BEFORE INSERT ON public.scheduling_authority_switch_events
FOR EACH ROW EXECUTE FUNCTION enforce_scheduling_authority_switch_event_actor();
