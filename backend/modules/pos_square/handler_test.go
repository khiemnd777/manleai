package pos_square

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/modules/booking"
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
	if body.Error.Message != "The request could not be completed." {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

func TestHandleGateErrorMapsSchedulingAuthorityNotReadyWithoutLeakingDetails(t *testing.T) {
	app := fiber.New()
	handler := &Handler{}
	diagnostic := "internal authority executor missing: owner_manual"
	app.Get("/", func(c *fiber.Ctx) error {
		_, err := handler.handleGateError(c, fmt.Errorf("%w: %s", booking.ErrSchedulingAuthorityNotReady, diagnostic), "SQUARE_TEST_BOOKING_FAILED")
		return err
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
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
	if body.Error.Code != "SCHEDULING_AUTHORITY_NOT_READY" {
		t.Fatalf("code = %q, want SCHEDULING_AUTHORITY_NOT_READY", body.Error.Code)
	}
	if body.Error.Message != "Scheduling is not ready for this salon." {
		t.Fatalf("message = %q", body.Error.Message)
	}
	if strings.Contains(body.Error.Message, diagnostic) {
		t.Fatalf("response leaked diagnostic detail: %q", body.Error.Message)
	}
}

func TestHandleGateErrorMapsAvailabilityQuoteConflicts(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code string
	}{
		{name: "required", err: booking.ErrAvailabilityQuoteRequired, code: "AVAILABILITY_QUOTE_REQUIRED"},
		{name: "stale", err: booking.ErrAvailabilityQuoteStale, code: "AVAILABILITY_QUOTE_STALE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			handler := &Handler{}
			app.Get("/", func(c *fiber.Ctx) error {
				_, err := handler.handleGateError(c, test.err, "SQUARE_TEST_BOOKING_FAILED")
				return err
			})

			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusConflict {
				t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode response failed: %v", err)
			}
			if body.Error.Code != test.code {
				t.Fatalf("code = %q, want %q", body.Error.Code, test.code)
			}
		})
	}
}
