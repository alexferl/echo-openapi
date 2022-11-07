# echo-openapi [![Go Report Card](https://goreportcard.com/badge/github.com/alexferl/echo-openapi)](https://goreportcard.com/report/github.com/alexferl/echo-openapi) [![codecov](https://codecov.io/gh/alexferl/echo-openapi/branch/master/graph/badge.svg)](https://codecov.io/gh/alexferl/echo-openapi)

An [OpenAPI](https://www.openapis.org/) middleware for the [Echo](https://github.com/labstack/echo) framework using
[getkin/kin-openapi](https://github.com/getkin/kin-openapi) to validate HTTP requests and responses.

## Installing
```shell
go get github.com/alexferl/echo-openapi
```

## Using

### Code example
```go
package main

import (
	"net/http"

	mw "github.com/alexferl/echo-openapi"
	"github.com/labstack/echo/v4"
)

/*
# openapi.yaml
openapi: 3.0.4
info:
  version: 1.0.0
  title: Test API
  description: A test API
paths:
  /hello:
    post:
      description: Hello
      parameters:
        - name: message
          in: query
          required: true
          schema:
            type: string
            minLength: 1
            maxLength: 100
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
                additionalProperties: false
                required:
                  - message
                properties:
                  message:
                    type: string
                    description: Welcome message
                    minLength: 4
*/

type Handler struct {
	*mw.Handler
}

func (h *Handler) Hello(c echo.Context) error {
	msg := c.QueryParam("message")
	return h.Validate(c, http.StatusOK, echo.Map{"message": msg})
}

func main() {
	e := echo.New()

	h := &Handler{mw.NewHandler()}
	e.Add(http.MethodPost, "/hello", h.Hello)

	e.Use(mw.OpenAPI("./path/to/openapi.yaml"))

	e.Logger.Fatal(e.Start("localhost:1323"))
}
```
Send an invalid request to test request validation:
```shell
curl -i -X POST http://localhost:1323/hello
HTTP/1.1 422 Unprocessable Entity
Content-Type: application/json; charset=UTF-8
Date: Mon, 07 Nov 2022 01:13:40 GMT
Content-Length: 117

{"message":"Validation error","errors":["parameter 'message' in query has an error: value is required but missing"]}
```

Send a valid request:
```shell
curl -i -X POST http://localhost:1323/hello\?message\=hello
HTTP/1.1 200 OK
Content-Type: application/json
Date: Mon, 07 Nov 2022 01:22:59 GMT
Content-Length: 19

{"message":"hello"}
```

Send a valid request with an invalid response:
```shell
curl -i -X POST http://localhost:1323/hello\?message\=a
HTTP/1.1 500 Internal Server Error
Content-Type: application/json
Date: Mon, 07 Nov 2022 01:16:43 GMT
Content-Length: 36

{"message":"Internal Server Error"}
```
You should also have the following in the server's log to help you debug your schema:
```shell
{"time":"2022-11-06T20:16:43.914629-05:00","level":"ERROR","prefix":"echo","file":"handler.go","line":"133","message":"response body doesn't match the schema: Error at \"/message\": minimum string length is 4\nSchema:\n  {\n    \"description\": \"Welcome message\",\n    \"minLength\": 4,\n    \"type\": \"string\"\n  }\n\nValue:\n  \"hi\"\n"}
```

### Configuration
```go
type Config struct {
	// Skipper defines a function to skip middleware.
	Skipper middleware.Skipper

	// Schema defines the OpenAPI that will be loaded and
	// that the request and responses will be validated against.
	// Required.
	Schema string

	// ContextKey defines the key that will be used to store the validator
	// on the echo.Context when the request is successfully validated.
	// Optional. Defaults to "validator".
	ContextKey string

	// ExemptRoutes defines routes and methods that don't require tokens.
	// Optional.
	ExemptRoutes map[string][]string
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

	// IncludeResponseStatus makes Validate fail on response
	// statuses not defined in the OpenAPI spec.
	// Optional. Defaults to true.
	IncludeResponseStatus bool
}
```
