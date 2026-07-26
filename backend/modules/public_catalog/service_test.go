package public_catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestGetBySlugNormalizesAndDelegates(t *testing.T) {
	store := &fakeStore{catalog: &Catalog{Salon: PublicSalon{Slug: "lotus-nails", Name: "Lotus Nails Studio"}}}
	service := NewService(store)

	catalog, err := service.GetBySlug(context.Background(), " Lotus-Nails ")
	if err != nil {
		t.Fatalf("GetBySlug returned error: %v", err)
	}
	if catalog == nil || catalog.Salon.Slug != "lotus-nails" {
		t.Fatalf("catalog = %#v, want lotus-nails", catalog)
	}
	if store.slug != "lotus-nails" {
		t.Fatalf("slug = %q, want normalized slug", store.slug)
	}
}

func TestGetBySlugRejectsInvalidSlug(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)

	_, err := service.GetBySlug(context.Background(), "../secret")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if store.slug != "" {
		t.Fatalf("store should not be called for invalid slug, got %q", store.slug)
	}
}

func TestGetFirstPublishedDelegates(t *testing.T) {
	store := &fakeStore{catalog: &Catalog{Salon: PublicSalon{Slug: "lotus-nails", Name: "Lotus Nails Studio"}}}
	service := NewService(store)

	catalog, err := service.GetFirstPublished(context.Background())
	if err != nil {
		t.Fatalf("GetFirstPublished returned error: %v", err)
	}
	if catalog == nil || catalog.Salon.Slug != "lotus-nails" {
		t.Fatalf("catalog = %#v, want lotus-nails", catalog)
	}
	if !store.firstPublishedCalled {
		t.Fatal("store GetFirstPublished was not called")
	}
}

func TestPublicCatalogJSONIsAuthorityAwareAndProviderSafe(t *testing.T) {
	encoded, err := json.Marshal(Catalog{
		Salon:                      PublicSalon{Slug: "lotus-nails", Name: "Lotus Nails", Phone: "555-0100"},
		SchedulingAuthority:        booking.SchedulingAuthorityOwnerManual,
		SchedulingAuthorityVersion: 3,
		Services:                   []PublicService{{Name: "Signature Manicure", DurationMinutes: 45}},
		Staff:                      []PublicStaffMember{},
		Hours:                      []PublicBusinessHourPeriod{},
		BookingNote:                "Call the salon to request an appointment.",
	})
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	value := string(encoded)
	for _, required := range []string{`"scheduling_authority":"owner_manual"`, `"scheduling_authority_version":3`, `"staff":[]`, `"hours":[]`} {
		if !strings.Contains(value, required) {
			t.Fatalf("public catalog JSON missing %s: %s", required, value)
		}
	}
	for _, forbidden := range []string{"active_pos_provider", "provider_entity_id", "owner_user_id", "pos_service_id"} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("public catalog JSON exposed %q: %s", forbidden, value)
		}
	}
}

type fakeStore struct {
	slug                 string
	firstPublishedCalled bool
	catalog              *Catalog
	err                  error
}

func (f *fakeStore) GetBySlug(ctx context.Context, slug string) (*Catalog, error) {
	f.slug = slug
	if f.err != nil {
		return nil, f.err
	}
	return f.catalog, nil
}

func (f *fakeStore) GetFirstPublished(ctx context.Context) (*Catalog, error) {
	f.firstPublishedCalled = true
	if f.err != nil {
		return nil, f.err
	}
	return f.catalog, nil
}
