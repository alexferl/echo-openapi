package openapi

import (
	"context"
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

	Schema       string
	ContextKey   string
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

func OpenAPIWithConfig(config Config) echo.MiddlewareFunc {
	if config.Skipper == nil {
		config.Skipper = DefaultConfig.Skipper
	}

	if config.ContextKey == "" {
		config.ContextKey = DefaultConfig.ContextKey
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			if check(c.Path(), c.Request().Method, config.ExemptRoutes) {
				return next(c)
			}

			ctx := context.Background()
			loader := &openapi3.Loader{Context: ctx, IsExternalRefsAllowed: true}
			schema, err := loader.LoadFromFile(config.Schema)
			if err != nil {
				c.Logger().Errorf("error loading schema file: %v", err)
				return err
			}

			err = schema.Validate(ctx)
			if err != nil {
				c.Logger().Errorf("error validating schema: %v", err)
				return err
			}

			r, err := gorillamux.NewRouter(schema)
			if err != nil {
				c.Logger().Errorf("error creating router: %v", err)
				return err
			}

			route, pathParams, err := r.FindRoute(c.Request())
			if err != nil {
				c.Logger().Debugf("error finding route for %s: %v", c.Request().URL.String(), err)

				if err == routers.ErrPathNotFound {
					return echo.NewHTTPError(http.StatusNotFound, "Path not found")
				}

				if err == routers.ErrMethodNotAllowed {
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
				return JSONValidationError(c, http.StatusUnprocessableEntity, "Validation error", errors)
			default:
				return JSONValidationError(c, http.StatusUnprocessableEntity, "Validation error", []string{err.Error()})
			}

			c.Set(config.ContextKey, requestValidationInput)

			return next(c)
		}
	}
}

func convertError(me openapi3.MultiError) map[string][]string {
	issues := make(map[string][]string)
	const schema = "schema"
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

			issues[schema] = append(issues[schema], msg)
		case *openapi3filter.RequestError: // possible there were multiple issues that failed validation
			// check if invalid HTTP parameter
			if err.Parameter != nil {
				prefix := err.Parameter.In
				name := fmt.Sprintf("%s.%s", prefix, err.Parameter.Name)
				issues[name] = append(issues[name], err.Error())
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
