package openapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Config struct {
	// Skipper defines a function to skip middleware.
	Skipper middleware.Skipper

	// Schema defines the OpenAPI that will be loaded and
	// that the requests and responses will be validated against.
	// Required.
	Schema string

	// SchemaBytes allows loading the OpenAPI specification directly
	// from a byte slice ([]byte). This is useful for embedding the
	// OpenAPI spec in the binary using Go's embed package, or if the
	// spec is obtained dynamically at runtime.
	// Required unless Schema is provided.
	//
	// If both Schema and SchemaBytes are provided, SchemaBytes takes precedence.
	SchemaBytes []byte

	// ContextKey defines the key that will be used to store the validator
	// on the echo.Context when the request is successfully validated.
	// Optional. Defaults to "validator".
	ContextKey string

	// ExemptRoutes defines routes and methods that don't require validation.
	// Optional.
	ExemptRoutes map[string][]string
}

var DefaultConfig = Config{
	Skipper:    middleware.DefaultSkipper,
	ContextKey: "validator",
}

func OpenAPI(file string) echo.MiddlewareFunc {
	c := DefaultConfig
	c.Schema = file
	return OpenAPIWithConfig(c)
}

func OpenAPIFromBytes(schemaBytes []byte) echo.MiddlewareFunc {
	c := DefaultConfig
	c.SchemaBytes = schemaBytes
	return OpenAPIWithConfig(c)
}

func OpenAPIWithConfig(config Config) echo.MiddlewareFunc {
	if config.Skipper == nil {
		config.Skipper = DefaultConfig.Skipper
	}

	if config.Schema == "" && len(config.SchemaBytes) == 0 {
		panic("either schema or schemaBytes is required")
	}

	if config.ContextKey == "" {
		config.ContextKey = DefaultConfig.ContextKey
	}

	ctx := context.Background()
	loader := &openapi3.Loader{Context: ctx, IsExternalRefsAllowed: true}

	var schema *openapi3.T
	var err error

	if len(config.SchemaBytes) > 0 {
		schema, err = loader.LoadFromData(config.SchemaBytes)
	} else {
		schema, err = loader.LoadFromFile(config.Schema)
	}

	if err != nil {
		panic(fmt.Sprintf("failed loading schema file: %v", err))
	}

	err = schema.Validate(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed validating schema: %v", err))
	}

	router, err := gorillamux.NewRouter(schema)
	if err != nil {
		panic(fmt.Sprintf("failed creating router: %v", err))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			if check(c.Path(), c.Request().Method, config.ExemptRoutes) {
				return next(c)
			}

			route, pathParams, err := router.FindRoute(c.Request())
			if err != nil {
				c.Logger().Debugf(
					"error finding route for %s %s: %v",
					c.Request().Method, c.Request().URL.String(), err,
				)

				if errors.Is(err, routers.ErrPathNotFound) {
					return echo.NewHTTPError(http.StatusNotFound, "Path not found")
				}

				if errors.Is(err, routers.ErrMethodNotAllowed) {
					return echo.NewHTTPError(http.StatusMethodNotAllowed, "Method not allowed")
				}

				return err
			}

			requestValidationInput := &openapi3filter.RequestValidationInput{
				Request:    c.Request(),
				PathParams: pathParams,
				Route:      route,
				Options: &openapi3filter.Options{
					MultiError:         true,
					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
				},
			}
			err = openapi3filter.ValidateRequest(ctx, requestValidationInput)
			switch err := err.(type) {
			case nil:
			case openapi3.MultiError:
				issues := convertError(err)
				names := make([]string, 0, len(issues))

				if val, ok := issues["body"]; ok {
					return JSONValidationError(c, http.StatusBadRequest, "Request error", val)
				}

				for k := range issues {
					names = append(names, k)
				}
				sort.Strings(names)
				var errs []string
				for _, k := range names {
					msgs := issues[k]
					for _, msg := range msgs {
						errs = append(errs, msg)
					}
				}
				return JSONValidationError(c, http.StatusUnprocessableEntity, "Validation error", errs)
			default:
				return err
			}

			c.Set(config.ContextKey, requestValidationInput)

			return next(c)
		}
	}
}

func convertError(me openapi3.MultiError) map[string][]string {
	issues := make(map[string][]string)
	for _, err := range me {
		switch err := err.(type) {
		case *openapi3.SchemaError:
			var field string
			if path := err.JSONPointer(); len(path) > 0 {
				field = strings.Join(path, ".")
			}

			var msg string
			if len(field) > 0 {
				msg = fmt.Sprintf("%s: %s", field, err.Reason)
			} else {
				msg = fmt.Sprintf("%s", err.Reason)
			}

			msg = strings.ReplaceAll(msg, "\"", "'")

			issues[field] = append(issues[field], msg)
		case *openapi3filter.RequestError: // possible there were multiple issues that failed validation
			// check if invalid HTTP parameter
			if err.Parameter != nil {
				prefix := err.Parameter.In
				name := fmt.Sprintf("%s.%s", prefix, err.Parameter.Name)
				split := strings.Split(err.Err.Error(), "\n")

				msg := fmt.Sprintf("parameter '%s' in %s has an error: %s", err.Parameter.Name, prefix, split[0])

				issues[name] = append(issues[name], msg)
				continue
			}

			if err, ok := err.Err.(openapi3.MultiError); ok {
				for k, v := range convertError(err) {
					issues[k] = append(issues[k], v...)
				}
				continue
			}

			// check if requestBody
			if err.RequestBody != nil {
				issues["body"] = append(issues["body"], err.Error())
				continue
			}
		default:
			const unknown = "unknown"
			issues[unknown] = append(issues[unknown], err.Error())
		}
	}
	return issues
}

func check(path string, method string, m map[string][]string) bool {
	for k, v := range m {
		if k == path {
			for _, i := range v {
				if method == i {
					return true
				}
			}
		}
	}
	return false
}

type ValidationError struct {
	echo.HTTPError
	Errors []string `json:"errors,omitempty"`
}

func JSONValidationError(c echo.Context, status int, msg string, errors []string) error {
	return c.JSON(status, ValidationError{
		echo.HTTPError{
			Code:    status,
			Message: msg,
		},
		errors,
	})
}
