package scheduling

import (
	"context"
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/salon"
)

type fakeSalonSettingsReader struct {
	settings    *salon.Settings
	err         error
	salonID     string
	ownerUserID string
	calls       int
}

func (f *fakeSalonSettingsReader) GetSettings(_ context.Context, salonID string, ownerUserID string) (*salon.Settings, error) {
	f.calls++
	f.salonID = salonID
	f.ownerUserID = ownerUserID
	return f.settings, f.err
}

func TestRepositoryResolvesOnlyOwnerScopedSalonSettingsAuthority(t *testing.T) {
	reader := &fakeSalonSettingsReader{settings: &salon.Settings{SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}}
	repository := newRepository(reader)

	authority, err := repository.ResolveSchedulingAuthority(context.Background(), "salon-1", "owner-1")
	if err != nil {
		t.Fatalf("ResolveSchedulingAuthority returned error: %v", err)
	}
	if authority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("authority = %q, want %q", authority, booking.SchedulingAuthorityExternalProvider)
	}
	if reader.calls != 1 || reader.salonID != "salon-1" || reader.ownerUserID != "owner-1" {
		t.Fatalf("settings lookup = calls:%d salon:%q owner:%q", reader.calls, reader.salonID, reader.ownerUserID)
	}
}

func TestRepositoryMapsOwnerScopedSettingsNotFoundToBookingNotFound(t *testing.T) {
	reader := &fakeSalonSettingsReader{err: salon.ErrNotFound}
	repository := newRepository(reader)

	_, err := repository.ResolveSchedulingAuthority(context.Background(), "salon-1", "other-owner")
	if !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("error = %v, want pos.ErrNotFound", err)
	}
	if reader.calls != 1 || reader.ownerUserID != "other-owner" {
		t.Fatalf("owner-scoped settings lookup = calls:%d owner:%q", reader.calls, reader.ownerUserID)
	}
}
