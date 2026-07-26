package notificationdelivery

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

type fakeHandlerService struct{ ownerUserID string }

func (*fakeHandlerService) ResolveAccessPrincipal(context.Context, string) (string, string, []string, error) {
	return "owner-1", "salon-1", []string{"salon_owner"}, nil
}

func (f *fakeHandlerService) List(_ context.Context, _, ownerUserID, _ string, limit, offset int) (*ListResponse, error) {
	f.ownerUserID = ownerUserID
	return &ListResponse{Deliveries: []DeliveryRecord{{ID: "notification-1", SalonID: "salon-1", NotificationType: "owner_manual_request_pending", InAppStatus: "pending", DeliveryStatus: StatusDeadLetter, DeliveryProvider: ProviderTwilio, DestinationMasked: "••••0123", ProviderStatus: "undelivered", LastDeliveryErrorCode: "TWILIO_30003", CanRequeue: true, NextDeliveryAt: time.Now().UTC(), CreatedAt: time.Now().UTC()}}, Limit: limit, Offset: offset}, nil
}
func (*fakeHandlerService) Get(context.Context, string, string, string) (*DetailResponse, error) {
	return nil, ErrNotFound
}
func (*fakeHandlerService) Requeue(context.Context, string, string, string, RequeueRequest) (*DetailResponse, bool, error) {
	return nil, false, ErrNotFound
}

func TestDeliveryRoutesRequireAuthenticationAndReturnPIISafeShape(t *testing.T) {
	const secret = "test-jwt-secret"
	service := &fakeHandlerService{}
	app := fiber.New()
	api := app.Group("/api", middleware.WithAccessPrincipalResolver(service))
	RegisterRoutes(api, NewHandler(service), secret)

	unauthenticated := httptest.NewRequest("GET", "/api/salons/salon-1/owner-notification-deliveries", nil)
	response, err := app.Test(unauthenticated)
	if err != nil || response.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d err=%v", response.StatusCode, err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.Claims{UserID: "owner-1", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}})
	signed, _ := token.SignedString([]byte(secret))
	request := httptest.NewRequest("GET", "/api/salons/salon-1/owner-notification-deliveries", nil)
	request.Header.Set("Authorization", "Bearer "+signed)
	response, err = app.Test(request)
	if err != nil || response.StatusCode != fiber.StatusOK {
		t.Fatalf("authenticated status=%d err=%v", response.StatusCode, err)
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw, _ := json.Marshal(payload)
	visible := string(raw)
	for _, forbidden := range []string{"+15555550123", "SM-provider-id", "Owner message body", "last_delivery_error\""} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("PII/provider detail leaked: %s", visible)
		}
	}
	if !strings.Contains(visible, "••••0123") || service.ownerUserID != "owner-1" {
		t.Fatalf("safe response=%s owner=%q", visible, service.ownerUserID)
	}
}
