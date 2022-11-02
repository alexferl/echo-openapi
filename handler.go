package openapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/labstack/echo/v4"
)

const (
	ApplicationJSON = "application/json"
	ApplicationXML  = "application/xml"
)

type Handler struct {
	Config HandlerConfig
}

type HandlerConfig struct {
	ContentType  string
	ValidatorKey string
	// Set ExcludeRequestBody so ValidateRequest skips request body validation.
	ExcludeRequestBody bool

	// Set ExcludeResponseBody so ValidateResponse skips response body validation.
	ExcludeResponseBody bool

	// Set IncludeResponseStatus so ValidateResponse fails on response
	// status not defined in OpenAPI spec.
	IncludeResponseStatus bool

	MultiError bool
}

var DefaultHandlerConfig = HandlerConfig{
	ContentType:           ApplicationJSON,
	ValidatorKey:          "validator",
	ExcludeRequestBody:    false,
	ExcludeResponseBody:   false,
	IncludeResponseStatus: true,
	MultiError:            true,
}

func NewHandler() *Handler {
	c := DefaultHandlerConfig
	return NewHandlerWithConfig(c)
}

func NewHandlerWithConfig(config HandlerConfig) *Handler {
	return &Handler{Config: config}
}

func (h *Handler) Validate(c echo.Context, code int, v any) error {
	return h.validate(c, code, h.Config.ContentType, v)
}

func (h *Handler) ValidateWithContentType(c echo.Context, code int, contentType string, v any) error {
	return h.validate(c, code, contentType, v)
}

func (h *Handler) validate(c echo.Context, code int, contentType string, v any) error {
	if code == http.StatusNoContent {
		return c.NoContent(code)
	}

	input, ok := c.Get(h.Config.ValidatorKey).(*openapi3filter.RequestValidationInput)
	if !ok {
		return fmt.Errorf("validator key is wrong type")
	}

	var (
		b   []byte
		err error
	)

	if strings.HasPrefix(contentType, ApplicationJSON) {
		b, err = json.Marshal(v)
	} else if strings.HasPrefix(contentType, ApplicationXML) {
		b, err = xml.Marshal(v)
	} else {
		switch v.(type) {
		case string:
			b = []byte(v.(string))
		}
	}

	if err != nil {
		return fmt.Errorf("failed marshaling response: %v", err)
	}

	c.Response().Status = code
	c.Response().Header().Add("Content-Type", contentType)

	responseValidationInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 c.Response().Status,
		Header:                 c.Response().Header(),
		Options: &openapi3filter.Options{
			ExcludeRequestBody:    h.Config.ExcludeRequestBody,
			ExcludeResponseBody:   h.Config.ExcludeResponseBody,
			IncludeResponseStatus: h.Config.IncludeResponseStatus,
			MultiError:            h.Config.MultiError,
		},
	}
	responseValidationInput.SetBodyBytes(b)

	ctx := context.Background()
	err = openapi3filter.ValidateResponse(ctx, responseValidationInput)
	if err != nil {
		c.Logger().Debug(err)
		return fmt.Errorf("failed validating response: %v", err)
	}

	return c.Blob(code, h.Config.ContentType, b)
}
