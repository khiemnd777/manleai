package respond

import "github.com/gofiber/fiber/v2"

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(c *fiber.Ctx, status int, body any) error {
	return c.Status(status).JSON(body)
}

func Error(c *fiber.Ctx, status int, code string, message string) error {
	return c.Status(status).JSON(ErrorResponse{
		Error: ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}
