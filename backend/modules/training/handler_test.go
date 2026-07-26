package training

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/middleware"
)

func TestEvaluateHandlerFailsUnknownAuthorityClosedWithGenericError(t *testing.T) {
	service := &Service{
		evaluationRepo:            &fakeEvaluationRepository{},
		schedulingAuthorityReader: &fakeEvaluationAuthorityReader{authority: "future_provider"},
	}
	response := executeTrainingEvaluationRequest(t, service, "owner-1")
	defer response.Body.Close()

	if response.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusInternalServerError)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "TRAINING_EVALUATION_FAILED" || payload.Error.Message != "Could not evaluate training question." {
		t.Fatalf("error = %#v, want generic evaluation failure", payload.Error)
	}
}

func TestEvaluateHandlerKeepsCrossTenantSalonNonEnumerating(t *testing.T) {
	authority := &fakeEvaluationAuthorityReader{authority: "external_provider"}
	service := &Service{
		evaluationRepo:            &fakeEvaluationRepository{ownerErr: ErrNotFound},
		schedulingAuthorityReader: authority,
	}
	response := executeTrainingEvaluationRequest(t, service, "other-owner")
	defer response.Body.Close()

	if response.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.StatusCode, fiber.StatusNotFound)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "SALON_NOT_FOUND" {
		t.Fatalf("error code = %q, want SALON_NOT_FOUND", payload.Error.Code)
	}
	if len(authority.calls) != 0 {
		t.Fatalf("authority calls = %#v, want none past tenant fence", authority.calls)
	}
}

func executeTrainingEvaluationRequest(t *testing.T, service *Service, ownerUserID string) *http.Response {
	t.Helper()
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, ownerUserID)
		return c.Next()
	})
	app.Post("/salons/:id/training/evaluate", NewHandler(service).Evaluate)

	request, err := http.NewRequest(http.MethodPost, "/salons/salon-1/training/evaluate", strings.NewReader(`{"message":"Do you take walk-ins?"}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	return response
}
