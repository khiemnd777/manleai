package business

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

const businessActionLockPrefix = "business-action:"

type mutationApply func(context.Context, *sql.Tx, string) error

func (r *Repository) applyMutation(ctx context.Context, command MutationCommand, create bool, apply mutationApply) (*MutationResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, businessActionLockPrefix+command.SalonID+":"+command.ActorUserID+":"+command.ActionKey); err != nil {
		return nil, err
	}

	var existingFingerprint, existingActionType, existingResourceType, existingResourceID string
	var existingVersion int64
	err = tx.QueryRowContext(ctx, `SELECT request_fingerprint,action_type,resource_type,resource_id,result_version FROM business_actions WHERE salon_id=$1 AND actor_user_id=$2 AND action_key=$3`, command.SalonID, command.ActorUserID, command.ActionKey).Scan(&existingFingerprint, &existingActionType, &existingResourceType, &existingResourceID, &existingVersion)
	if err == nil {
		if existingFingerprint != command.RequestFingerprint || existingActionType != command.ActionType || existingResourceType != command.ResourceType {
			return nil, ErrActionConflict
		}
		return &MutationResult{ResourceType: existingResourceType, ResourceID: existingResourceID, Version: existingVersion, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if command.SchedulingFence {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fence.AdvisoryKey(command.SalonID)); err != nil {
			return nil, err
		}
	}

	previousVersion := int64(0)
	if create {
		if command.ExpectedVersion != 0 {
			return nil, ErrVersionConflict
		}
	} else {
		err = tx.QueryRowContext(ctx, `SELECT version FROM business_resource_versions WHERE salon_id=$1 AND resource_type=$2 AND resource_id=$3 FOR UPDATE`, command.SalonID, command.ResourceType, command.ResourceID).Scan(&previousVersion)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if previousVersion != command.ExpectedVersion {
			return nil, ErrVersionConflict
		}
	}
	if err := apply(ctx, tx, command.ResourceID); err != nil {
		return nil, mapMutationError(err)
	}
	resultVersion := previousVersion + 1
	if create {
		_, err = tx.ExecContext(ctx, `INSERT INTO business_resource_versions(salon_id,resource_type,resource_id,version,updated_by_user_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(salon_id,resource_type,resource_id) DO UPDATE SET updated_by_user_id=EXCLUDED.updated_by_user_id,updated_at=now()`, command.SalonID, command.ResourceType, command.ResourceID, resultVersion, command.ActorUserID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE business_resource_versions SET version=$1,updated_by_user_id=$2,updated_at=now() WHERE salon_id=$3 AND resource_type=$4 AND resource_id=$5`, resultVersion, command.ActorUserID, command.SalonID, command.ResourceType, command.ResourceID)
	}
	if err != nil {
		return nil, mapMutationError(err)
	}
	response, err := json.Marshal(map[string]any{"resource_type": command.ResourceType, "resource_id": command.ResourceID, "version": resultVersion})
	if err != nil {
		return nil, err
	}
	var actionID string
	err = tx.QueryRowContext(ctx, `INSERT INTO business_actions(salon_id,actor_user_id,surface,action_key,action_type,request_fingerprint,resource_type,resource_id,previous_version,result_version,response_payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id::text`, command.SalonID, command.ActorUserID, string(command.Surface), command.ActionKey, command.ActionType, command.RequestFingerprint, command.ResourceType, command.ResourceID, previousVersion, resultVersion, response).Scan(&actionID)
	if err != nil {
		return nil, mapMutationError(err)
	}
	details, err := json.Marshal(map[string]any{"changed_fields": command.ChangedFields})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO business_events(action_id,salon_id,actor_user_id,surface,event_type,resource_type,resource_id,previous_version,result_version,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, actionID, command.SalonID, command.ActorUserID, string(command.Surface), command.ActionType, command.ResourceType, command.ResourceID, previousVersion, resultVersion, details); err != nil {
		return nil, mapMutationError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &MutationResult{ResourceType: command.ResourceType, ResourceID: command.ResourceID, Version: resultVersion}, nil
}

func mapMutationError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrValidation) || errors.Is(err, ErrProviderReadOnly) || errors.Is(err, ErrPublicationBlocked) {
		return err
	}
	return mapWriteError(err)
}

func (r *Repository) MutateSalonProfile(ctx context.Context, command MutationCommand, req SalonProfileMutationRequest) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, _ string) error {
		result, err := tx.ExecContext(ctx, `UPDATE salons SET name=$1,phone=$2,address=NULLIF($3,''),city=NULLIF($4,''),state=NULLIF($5,''),zip_code=NULLIF($6,''),timezone=$7,primary_language=$8,secondary_language=$9,handoff_phone=NULLIF($10,''),updated_at=now() WHERE id=$11`, req.Name, req.Phone, req.Address, req.City, req.State, req.ZipCode, req.Timezone, req.PrimaryLanguage, req.SecondaryLanguage, req.HandoffPhone, command.SalonID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) MutateService(ctx context.Context, command MutationCommand, req ServiceMutationRequest, create bool) (*MutationResult, error) {
	return r.applyMutation(ctx, command, create, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		if create {
			name := strings.TrimSpace(*req.Name)
			duration := *req.DurationMinutes
			aiBookable := true
			if req.AIBookable != nil {
				aiBookable = *req.AIBookable
			}
			active := true
			if req.Active != nil {
				active = *req.Active
			}
			if aiBookable && (!active || duration <= 0) {
				return ErrValidation
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO services(id,salon_id,pos_provider,pos_service_id,pos_service_version,name,description,ai_description,duration_minutes,price_from,price_display,ai_bookable,active,sync_status,source,service_category_id,service_category_source,service_category_reviewed_by,service_category_reviewed_at) SELECT $1,salon.id,salon.active_pos_provider,NULL,NULL,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,NULLIF($7,''),$8,$9,'local_only','local',NULLIF($10,'')::uuid,CASE WHEN NULLIF($10,'') IS NULL THEN 'unassigned' ELSE 'manual' END,CASE WHEN NULLIF($10,'') IS NULL THEN NULL ELSE $11::uuid END,CASE WHEN NULLIF($10,'') IS NULL THEN NULL ELSE now() END FROM salons salon WHERE salon.id=$12`, resourceID, name, stringValue(req.Description), stringValue(req.AIDescription), duration, nullableFloat(req.PriceFrom), stringValue(req.PriceDisplay), aiBookable, active, stringValue(req.ServiceCategoryID), command.ActorUserID, command.SalonID)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO pos_entity_links(salon_id,entity_type,entity_id,provider,sync_status) SELECT id,'service',$1,active_pos_provider,'local_only' FROM salons WHERE id=$2`, resourceID, command.SalonID); err != nil {
				return err
			}
		} else {
			if operationalServiceMutation(req) {
				readOnly, err := providerReadOnlyTx(ctx, tx, command.SalonID, "service", resourceID)
				if err != nil {
					return err
				}
				if readOnly {
					return ErrProviderReadOnly
				}
			}
			if req.AIBookable != nil && *req.AIBookable {
				if err := validateServiceAIBookableTx(ctx, tx, command.SalonID, resourceID, req); err != nil {
					return err
				}
			}
			result, err := tx.ExecContext(ctx, `UPDATE services SET name=COALESCE($1,name),description=CASE WHEN $2::boolean THEN NULLIF($3,'') ELSE description END,ai_description=CASE WHEN $4::boolean THEN NULLIF($5,'') ELSE ai_description END,duration_minutes=COALESCE($6,duration_minutes),price_from=COALESCE($7,price_from),price_display=CASE WHEN $8::boolean THEN NULLIF($9,'') ELSE price_display END,ai_bookable=COALESCE($10,ai_bookable),active=COALESCE($11,active),service_category_id=CASE WHEN $12::boolean THEN NULLIF($13,'')::uuid ELSE service_category_id END,service_category_source=CASE WHEN $12::boolean THEN CASE WHEN NULLIF($13,'') IS NULL THEN 'unassigned' ELSE 'manual' END ELSE service_category_source END,service_category_reviewed_by=CASE WHEN $12::boolean AND NULLIF($13,'') IS NOT NULL THEN $14::uuid WHEN $12::boolean THEN NULL ELSE service_category_reviewed_by END,service_category_reviewed_at=CASE WHEN $12::boolean AND NULLIF($13,'') IS NOT NULL THEN now() WHEN $12::boolean THEN NULL ELSE service_category_reviewed_at END,updated_at=now() WHERE salon_id=$15 AND id=$16 AND archived_at IS NULL`, nullableString(req.Name), req.Description != nil, stringValue(req.Description), req.AIDescription != nil, stringValue(req.AIDescription), nullableInt(req.DurationMinutes), nullableFloat(req.PriceFrom), req.PriceDisplay != nil, stringValue(req.PriceDisplay), nullableBool(req.AIBookable), nullableBool(req.Active), req.ServiceCategoryID != nil, stringValue(req.ServiceCategoryID), command.ActorUserID, command.SalonID, resourceID)
			if err != nil {
				return err
			}
			if err := requireAffected(result); err != nil {
				return err
			}
		}
		if req.ConsultationProfile != nil {
			profile := req.ConsultationProfile
			return pos.UpsertServiceConsultationProfileTx(ctx, tx, command.SalonID, resourceID, command.ActorUserID, pos.ServiceConsultationProfileMutation{
				Status: profile.Status, RecommendedOutcomes: profile.RecommendedOutcomes,
				CompatibleCurrentSystems: profile.CompatibleCurrentSystems,
				LengthCapabilities:       profile.LengthCapabilities, PriorityTags: profile.PriorityTags,
				FinishOptions: profile.FinishOptions, MaintenanceNote: profile.MaintenanceNote,
				OwnerApprovedSummary: profile.OwnerApprovedSummary,
			})
		}
		return nil
	})
}

func (r *Repository) ArchiveService(ctx context.Context, command MutationCommand) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		readOnly, err := providerReadOnlyTx(ctx, tx, command.SalonID, "service", resourceID)
		if err != nil {
			return err
		}
		if readOnly {
			return ErrProviderReadOnly
		}
		result, err := tx.ExecContext(ctx, `UPDATE services SET active=false,ai_bookable=false,sync_status='archived',archived_at=COALESCE(archived_at,now()),updated_at=now() WHERE salon_id=$1 AND id=$2`, command.SalonID, resourceID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) MutateServiceCategory(ctx context.Context, command MutationCommand, req ServiceCategoryMutationRequest, create bool) (*MutationResult, error) {
	return r.applyMutation(ctx, command, create, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		if create {
			_, err := tx.ExecContext(ctx, `INSERT INTO service_categories(id,salon_id,name,slug,description,sort_order,source,reviewed_by,reviewed_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,'manual',$7,now())`, resourceID, command.SalonID, req.Name, req.Slug, req.Description, req.SortOrder, command.ActorUserID)
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE service_categories SET name=$1,slug=$2,description=NULLIF($3,''),sort_order=$4,reviewed_by=$5,reviewed_at=now(),updated_at=now() WHERE salon_id=$6 AND id=$7 AND archived_at IS NULL`, req.Name, req.Slug, req.Description, req.SortOrder, command.ActorUserID, command.SalonID, resourceID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) ArchiveServiceCategory(ctx context.Context, command MutationCommand) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		result, err := tx.ExecContext(ctx, `UPDATE service_categories SET status='archived',archived_at=COALESCE(archived_at,now()),updated_at=now() WHERE salon_id=$1 AND id=$2`, command.SalonID, resourceID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) MutateStaff(ctx context.Context, command MutationCommand, req StaffMutationRequest, create bool) (*MutationResult, error) {
	return r.applyMutation(ctx, command, create, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		if create {
			name := strings.TrimSpace(*req.Name)
			aiBookable := true
			if req.AIBookable != nil {
				aiBookable = *req.AIBookable
			}
			active := true
			if req.Active != nil {
				active = *req.Active
			}
			if aiBookable && !active {
				return ErrValidation
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO staff(id,salon_id,pos_provider,pos_staff_id,name,phone,email,ai_bookable,active,sync_status,source) SELECT $1,id,active_pos_provider,NULL,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,'local_only','local' FROM salons WHERE id=$7`, resourceID, name, stringValue(req.Phone), stringValue(req.Email), aiBookable, active, command.SalonID)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO pos_entity_links(salon_id,entity_type,entity_id,provider,sync_status) SELECT id,'staff',$1,active_pos_provider,'local_only' FROM salons WHERE id=$2`, resourceID, command.SalonID)
			return err
		}
		if operationalStaffMutation(req) {
			readOnly, err := providerReadOnlyTx(ctx, tx, command.SalonID, "staff", resourceID)
			if err != nil {
				return err
			}
			if readOnly {
				return ErrProviderReadOnly
			}
		}
		if req.AIBookable != nil && *req.AIBookable {
			if err := validateStaffAIBookableTx(ctx, tx, command.SalonID, resourceID, req); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE staff SET name=COALESCE($1,name),phone=CASE WHEN $2::boolean THEN NULLIF($3,'') ELSE phone END,email=CASE WHEN $4::boolean THEN NULLIF($5,'') ELSE email END,ai_bookable=COALESCE($6,ai_bookable),active=COALESCE($7,active),updated_at=now() WHERE salon_id=$8 AND id=$9 AND archived_at IS NULL`, nullableString(req.Name), req.Phone != nil, stringValue(req.Phone), req.Email != nil, stringValue(req.Email), nullableBool(req.AIBookable), nullableBool(req.Active), command.SalonID, resourceID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) ArchiveStaff(ctx context.Context, command MutationCommand) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		readOnly, err := providerReadOnlyTx(ctx, tx, command.SalonID, "staff", resourceID)
		if err != nil {
			return err
		}
		if readOnly {
			return ErrProviderReadOnly
		}
		result, err := tx.ExecContext(ctx, `UPDATE staff SET active=false,ai_bookable=false,sync_status='archived',archived_at=COALESCE(archived_at,now()),updated_at=now() WHERE salon_id=$1 AND id=$2`, command.SalonID, resourceID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) ReplaceStaffServiceEligibility(ctx context.Context, command MutationCommand, staffID string, serviceIDs []string) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, _ string) error {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM staff WHERE salon_id=$1 AND id=$2 AND active AND archived_at IS NULL)`, command.SalonID, staffID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrNotFound
		}
		if len(serviceIDs) > 0 {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM services WHERE salon_id=$1 AND id=ANY($2::uuid[]) AND active AND archived_at IS NULL`, command.SalonID, pqStringArray(serviceIDs)).Scan(&count); err != nil {
				return err
			}
			if count != len(serviceIDs) {
				return ErrNotFound
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM manleai_calendar_service_staff WHERE salon_id=$1 AND staff_id=$2`, command.SalonID, staffID); err != nil {
			return err
		}
		for _, serviceID := range serviceIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO manleai_calendar_service_staff(salon_id,service_id,staff_id) VALUES($1,$2,$3)`, command.SalonID, serviceID, staffID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) ReplaceBusinessHours(ctx context.Context, command MutationCommand, periods []BusinessHourPeriodInput) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, _ string) error {
		var authority string
		if err := tx.QueryRowContext(ctx, `SELECT scheduling_authority FROM salon_settings WHERE salon_id=$1 FOR UPDATE`, command.SalonID).Scan(&authority); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if authority == "external_provider" {
			return ErrProviderReadOnly
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM salon_business_hour_periods WHERE salon_id=$1 AND source='local_override'`, command.SalonID); err != nil {
			return err
		}
		indexes := map[int]int{}
		for _, period := range periods {
			indexes[period.DayOfWeek]++
			if _, err := tx.ExecContext(ctx, `INSERT INTO salon_business_hour_periods(salon_id,day_of_week,start_local_time,end_local_time,end_at_midnight,source,provider,provider_location_id,provider_period_index,last_synced_at) VALUES($1,$2,$3::time,$4::time,$5,'local_override','','',$6,NULL)`, command.SalonID, period.DayOfWeek, period.StartLocalTime, period.EndLocalTime, period.EndAtMidnight, indexes[period.DayOfWeek]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) MutatePublicCatalogSettings(ctx context.Context, command MutationCommand, req PublicCatalogMutationRequest) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, _ string) error {
		if req.PublicCatalogEnabled {
			readiness, err := salon.PublicCatalogSettingsForSalon(ctx, tx, command.SalonID)
			if err != nil {
				if errors.Is(err, salon.ErrNotFound) {
					return ErrNotFound
				}
				return err
			}
			blocked := false
			for _, blocker := range readiness.ReadinessBlockers {
				if blocker.Code != "PUBLIC_SLUG_REQUIRED" {
					blocked = true
					break
				}
			}
			if req.PublicSlug == "" || blocked {
				return ErrPublicationBlocked
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE salons SET public_slug=NULLIF($1,''),public_catalog_enabled=$2,updated_at=now() WHERE id=$3`, req.PublicSlug, req.PublicCatalogEnabled, command.SalonID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) MutateCustomer(ctx context.Context, command MutationCommand, req CustomerMutationRequest, create bool) (*MutationResult, error) {
	return r.applyMutation(ctx, command, create, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		if create {
			name := strings.TrimSpace(*req.Name)
			active := true
			if req.Active != nil {
				active = *req.Active
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO customers(id,salon_id,name,phone,normalized_phone,email,normalized_email,notes,active,sync_status,source) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($4,''),NULLIF($5,''),NULLIF($5,''),NULLIF($6,''),$7,'local_only','local')`, resourceID, command.SalonID, name, stringValue(req.Phone), stringValue(req.Email), stringValue(req.Notes), active)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO pos_entity_links(salon_id,entity_type,entity_id,provider,sync_status) SELECT id,'customer',$1,active_pos_provider,'local_only' FROM salons WHERE id=$2`, resourceID, command.SalonID)
			return err
		}
		readOnly, err := providerReadOnlyTx(ctx, tx, command.SalonID, "customer", resourceID)
		if err != nil {
			return err
		}
		if readOnly {
			return ErrProviderReadOnly
		}
		result, err := tx.ExecContext(ctx, `UPDATE customers SET name=COALESCE($1,name),phone=CASE WHEN $2::boolean THEN NULLIF($3,'') ELSE phone END,normalized_phone=CASE WHEN $2::boolean THEN NULLIF($3,'') ELSE normalized_phone END,email=CASE WHEN $4::boolean THEN NULLIF($5,'') ELSE email END,normalized_email=CASE WHEN $4::boolean THEN NULLIF($5,'') ELSE normalized_email END,notes=CASE WHEN $6::boolean THEN NULLIF($7,'') ELSE notes END,active=COALESCE($8,active),updated_at=now() WHERE salon_id=$9 AND id=$10 AND archived_at IS NULL`, nullableString(req.Name), req.Phone != nil, stringValue(req.Phone), req.Email != nil, stringValue(req.Email), req.Notes != nil, stringValue(req.Notes), nullableBool(req.Active), command.SalonID, resourceID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func (r *Repository) ArchiveCustomer(ctx context.Context, command MutationCommand) (*MutationResult, error) {
	return r.applyMutation(ctx, command, false, func(ctx context.Context, tx *sql.Tx, resourceID string) error {
		readOnly, err := providerReadOnlyTx(ctx, tx, command.SalonID, "customer", resourceID)
		if err != nil {
			return err
		}
		if readOnly {
			return ErrProviderReadOnly
		}
		result, err := tx.ExecContext(ctx, `UPDATE customers SET active=false,sync_status='archived',archived_at=COALESCE(archived_at,now()),updated_at=now() WHERE salon_id=$1 AND id=$2`, command.SalonID, resourceID)
		if err != nil {
			return err
		}
		return requireAffected(result)
	})
}

func providerReadOnlyTx(ctx context.Context, tx *sql.Tx, salonID, entityType, entityID string) (bool, error) {
	var value bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons salon JOIN pos_entity_links link ON link.salon_id=salon.id AND link.provider=salon.active_pos_provider WHERE salon.id=$1 AND link.entity_type=$2 AND link.entity_id=$3 AND link.sync_status='synced' AND NULLIF(link.provider_entity_id,'') IS NOT NULL)`, salonID, entityType, entityID).Scan(&value)
	return value, err
}

func validateServiceAIBookableTx(ctx context.Context, tx *sql.Tx, salonID, serviceID string, req ServiceMutationRequest) error {
	var authority string
	var active bool
	var archived bool
	var duration int
	var linked bool
	err := tx.QueryRowContext(ctx, `SELECT settings.scheduling_authority,COALESCE($3,service.active),service.archived_at IS NOT NULL,COALESCE($4,service.duration_minutes),EXISTS(SELECT 1 FROM salons salon JOIN pos_entity_links link ON link.salon_id=salon.id AND link.provider=salon.active_pos_provider AND link.entity_type='service' AND link.entity_id=service.id WHERE salon.id=service.salon_id AND link.sync_status='synced' AND NULLIF(link.provider_entity_id,'') IS NOT NULL) FROM services service JOIN salon_settings settings ON settings.salon_id=service.salon_id WHERE service.salon_id=$1 AND service.id=$2`, salonID, serviceID, nullableBool(req.Active), nullableInt(req.DurationMinutes)).Scan(&authority, &active, &archived, &duration, &linked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if archived || !active || duration <= 0 || authority == "external_provider" && !linked {
		return ErrValidation
	}
	return nil
}

func validateStaffAIBookableTx(ctx context.Context, tx *sql.Tx, salonID, staffID string, req StaffMutationRequest) error {
	var authority string
	var active, archived, linked bool
	err := tx.QueryRowContext(ctx, `SELECT settings.scheduling_authority,COALESCE($3,staff.active),staff.archived_at IS NOT NULL,EXISTS(SELECT 1 FROM salons salon JOIN pos_entity_links link ON link.salon_id=salon.id AND link.provider=salon.active_pos_provider AND link.entity_type='staff' AND link.entity_id=staff.id WHERE salon.id=staff.salon_id AND link.sync_status='synced' AND NULLIF(link.provider_entity_id,'') IS NOT NULL) FROM staff staff JOIN salon_settings settings ON settings.salon_id=staff.salon_id WHERE staff.salon_id=$1 AND staff.id=$2`, salonID, staffID, nullableBool(req.Active)).Scan(&authority, &active, &archived, &linked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if archived || !active || authority == "external_provider" && !linked {
		return ErrValidation
	}
	return nil
}

func operationalServiceMutation(req ServiceMutationRequest) bool {
	return req.Name != nil || req.Description != nil || req.DurationMinutes != nil || req.PriceFrom != nil || req.PriceDisplay != nil || req.Active != nil
}
func operationalStaffMutation(req StaffMutationRequest) bool {
	return req.Name != nil || req.Phone != nil || req.Email != nil || req.Active != nil
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

type pqStringArray []string

func (a pqStringArray) Value() (driver.Value, error) { return pq.Array([]string(a)).Value() }
