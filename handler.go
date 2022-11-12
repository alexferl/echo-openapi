package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/labstack/echo/v4"
)

const (
	ApplicationJSON = echo.MIMEApplicationJSON
)

type Handler struct {
	Config HandlerConfig
}

type HandlerConfig struct {
	// ContentType sets the Content-Type header of the response.
	// Optional. Defaults to "application/json".
	ContentType string

	// ValidatorKey defines the key that will be used to read the
	// *openapi3filter.RequestValidationInput from the echo.Context
	// set by the middleware.
	// Optional. Defaults to "validator".
	ValidatorKey string

	// ExcludeRequestBody makes Validate skips request body validation.
	// Optional. Defaults to false.
	ExcludeRequestBody bool

	// ExcludeResponseBody makes Validate skips response body validation.
	// Optional. Defaults to false.
	ExcludeResponseBody bool

	// IncludeResponseStatus so ValidateResponse fails on response
	// statuses not defined in the OpenAPI spec.
	// Optional. Defaults to true.
	IncludeResponseStatus bool
}

var DefaultHandlerConfig = HandlerConfig{
	ContentType:           ApplicationJSON,
	ValidatorKey:          "validator",
	ExcludeRequestBody:    false,
	ExcludeResponseBody:   false,
	IncludeResponseStatus: true,
}

func NewHandler() *Handler {
	c := DefaultHandlerConfig
	return NewHandlerWithConfig(c)
}

func NewHandlerWithConfig(config HandlerConfig) *Handler {
	if config.ContentType == "" {
		config.ContentType = DefaultHandlerConfig.ContentType
	}

	if config.ValidatorKey == "" {
		config.ValidatorKey = DefaultHandlerConfig.ValidatorKey
	}

	return &Handler{Config: config}
}

func (h *Handler) Validate(c echo.Context, code int, v any) error {
	return h.validate(c, code, h.Config.ContentType, v)
}

func (h *Handler) ValidateWithContentType(c echo.Context, code int, contentType string, v any) error {
	return h.validate(c, code, contentType, v)
}

func (h *Handler) validate(c echo.Context, code int, contentType string, v any) error {
	// there's nothing to validate so just return
	if code == http.StatusNoContent {
		return c.NoContent(code)
	}

	c.Response().Status = code

	input, ok := c.Get(h.Config.ValidatorKey).(*openapi3filter.RequestValidationInput)
	if !ok {
		return fmt.Errorf("validator key is wrong type")
	}

	var (
		b   []byte
		err error
	)

	if strings.HasPrefix(contentType, ApplicationJSON) {
		c.Response().Header().Add("Content-Type", contentType)
		b, err = json.Marshal(v)
	} else {
		c.Response().Header().Add("Content-Type", echo.MIMETextPlain)
		switch t := v.(type) {
		case string:
			b = []byte(v.(string))
		case []byte:
			b = v.([]byte)
		default:
			return fmt.Errorf("type %s not supported", t)
		}
	}

	if err != nil {
		return fmt.Errorf("failed marshaling response: %v", err)
	}

	responseValidationInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 c.Response().Status,
		Header:                 c.Response().Header(),
		Options: &openapi3filter.Options{
			ExcludeRequestBody:    h.Config.ExcludeRequestBody,
			ExcludeResponseBody:   h.Config.ExcludeResponseBody,
			IncludeResponseStatus: h.Config.IncludeResponseStatus,
			MultiError:            true,
		},
	}
	responseValidationInput.SetBodyBytes(b)

	ctx := context.Background()
	err = openapi3filter.ValidateResponse(ctx, responseValidationInput)
	if err != nil {
		switch err := err.(type) {
		case nil:
		case *openapi3filter.ResponseError:
			if me, ok := err.Err.(openapi3.MultiError); ok {
				issues := convertError(me)
				names := make([]string, 0, len(issues))

				for k := range issues {
					names = append(names, k)
				}
				sort.Strings(names)
				var errors []string
				for _, k := range names {
					msgs := issues[k]
					for _, msg := range msgs {
						errors = append(errors, msg)
					}
				}

				return fmt.Errorf("failed validating response: %s", strings.Join(errors, "; "))
			}
		default:
			return fmt.Errorf("failed validating response: %v", err)
		}
	}

	return c.Blob(code, h.Config.ContentType, b)
}
