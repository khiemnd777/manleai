package pos_square

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestHandleGateErrorMarksErrorAsHandledWhenResponseWriteSucceeds(t *testing.T) {
	app := fiber.New()
	handler := &Handler{}
	handledSeen := false

	app.Get("/", func(c *fiber.Ctx) error {
		handled, err := handler.handleGateError(c, errors.New("booking service did not return a booking attempt"), "SQUARE_TEST_BOOKING_FAILED")
		handledSeen = handled
		return err
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if !handledSeen {
		t.Fatalf("expected gate error to be marked handled")
	}
	if resp.StatusCode != fiber.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadGateway)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if body.Error.Code != "SQUARE_TEST_BOOKING_FAILED" {
		t.Fatalf("code = %q, want SQUARE_TEST_BOOKING_FAILED", body.Error.Code)
	}
	if body.Error.Message != "booking service did not return a booking attempt" {
		t.Fatalf("message = %q", body.Error.Message)
	}
}
