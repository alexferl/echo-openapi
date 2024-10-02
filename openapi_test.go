package openapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestOpenAPIWithConfig_Schema_Load_Panics(t *testing.T) {
	testCases := []struct {
		name string
		conf Config
	}{
		{"no schema", Config{}},
		{"invalid schema", Config{Schema: "./fixtures/invalid.yaml"}},
		{"invalid path", Config{Schema: "/invalid/path"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			assert.Panics(t, func() { e.Use(OpenAPIWithConfig(tc.conf)) })
		})
	}
}

func TestOpenAPIWithConfig_Skipper(t *testing.T) {
	e := echo.New()

	e.GET("/", func(c echo.Context) error {
		return c.JSON(http.StatusOK, "ok")
	})

	e.Use(OpenAPIWithConfig(Config{
		Skipper: func(c echo.Context) bool { return true },
		Schema:  "./fixtures/openapi.yaml",
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	e.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestOpenAPIWithConfig_Defaults(t *testing.T) {
	e := echo.New()

	e.GET("/", func(c echo.Context) error {
		return c.JSON(http.StatusOK, "ok")
	})

	e.Use(OpenAPI("./fixtures/openapi.yaml"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	e.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestOpenAPIWithConfig_ExemptRoutes(t *testing.T) {
	e := echo.New()

	e.GET("/exempt", func(c echo.Context) error {
		return c.JSON(http.StatusOK, "ok")
	})

	e.Use(OpenAPIWithConfig(Config{
		Schema: "./fixtures/openapi.yaml",
		ExemptRoutes: map[string][]string{
			"/exempt": {http.MethodGet},
		},
	}))

	req := httptest.NewRequest(http.MethodGet, "/exempt", nil)
	resp := httptest.NewRecorder()

	e.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestOpenAPIWithConfig_FindRoute(t *testing.T) {
	testCases := []struct {
		name       string
		path       string
		method     string
		statusCode int
	}{
		{"path not found", "/notfound", http.MethodGet, http.StatusNotFound},
		{"method not allowed", "/", http.MethodPost, http.StatusMethodNotAllowed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			e.Any(tc.path, func(c echo.Context) error {
				return c.JSON(http.StatusOK, "ok")
			})

			e.Use(OpenAPI("./fixtures/openapi.yaml"))

			req := httptest.NewRequest(tc.method, tc.path, nil)
			resp := httptest.NewRecorder()

			e.ServeHTTP(resp, req)

			assert.Equal(t, tc.statusCode, resp.Code)
		})
	}
}

func TestOpenAPIWithConfig_Request_Validation(t *testing.T) {
	testCases := []struct {
		name        string
		path        string
		body        *bytes.Buffer
		contentType bool
		header      string
		statusCode  int
		errors      []string
	}{
		{
			name:       "no body",
			path:       "/validation",
			body:       bytes.NewBuffer([]byte(``)),
			statusCode: http.StatusBadRequest,
			errors:     []string{"request body has an error: value is required but missing"},
		},
		{
			name:       "body error",
			path:       "/validation",
			body:       bytes.NewBuffer([]byte(`{}`)),
			statusCode: http.StatusUnprocessableEntity,
			errors:     []string{"username: property 'username' is missing"},
		},
		{
			name:       "body error multiple",
			path:       "/validation",
			body:       bytes.NewBuffer([]byte(`{"username":1, "invalid": "value"}`)),
			statusCode: http.StatusUnprocessableEntity,
			errors: []string{
				"property 'invalid' is unsupported",
				"username: value must be a string",
			},
		},
		{
			name:       "path error",
			path:       "/validation/a",
			body:       bytes.NewBuffer([]byte(`{"username": "test"}`)),
			statusCode: http.StatusUnprocessableEntity,
			errors:     []string{"parameter 'username' in path has an error: minimum string length is 2"},
		},
		{
			name:       "query error",
			path:       "/validation/test?limit=200",
			body:       bytes.NewBuffer([]byte(`{"username": "test"}`)),
			statusCode: http.StatusUnprocessableEntity,
			errors:     []string{"parameter 'limit' in query has an error: number must be at most 100"},
		},
		{
			name:       "header error",
			path:       "/validation/test",
			body:       bytes.NewBuffer([]byte(`{"username": "test"}`)),
			header:     "x-username",
			statusCode: http.StatusUnprocessableEntity,
			errors:     []string{"parameter 'x-username' in header has an error: minimum string length is 2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			e.Any("/validation", func(c echo.Context) error {
				return c.JSON(http.StatusOK, "ok")
			})

			e.Any("/validation/:username", func(c echo.Context) error {
				return c.JSON(http.StatusOK, "ok")
			})

			e.Use(OpenAPI("./fixtures/openapi.yaml"))

			req := httptest.NewRequest(http.MethodPost, tc.path, tc.body)
			if !tc.contentType {
				req.Header.Add("Content-Type", echo.MIMEApplicationJSON)
			}
			if tc.header != "" {
				req.Header.Add(tc.header, "a")
			}

			resp := httptest.NewRecorder()

			e.ServeHTTP(resp, req)

			b, err := io.ReadAll(resp.Result().Body)
			assert.NoError(t, err)
			defer resp.Result().Body.Close()

			j := &ValidationError{}
			err = json.Unmarshal(b, j)
			assert.NoError(t, err)

			assert.Equal(t, tc.statusCode, resp.Code)
			assert.ElementsMatch(t, j.Errors, tc.errors)
		})
	}
}

func TestOpenAPIFromBytes(t *testing.T) {
	sampleOpenAPISpec := []byte(`
openapi: 3.0.0
info:
  title: Sample API
  version: "1.0"
paths:
  /:
    get:
      responses:
        '200':
          description: OK
  /validation:
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                username:
                  type: string
                  minLength: 2
      responses:
        '200':
          description: OK
`)

	testCases := []struct {
		name        string
		method      string
		path        string
		body        *bytes.Buffer
		statusCode  int
		contentType string
	}{
		{
			name:       "valid GET request to /",
			method:     http.MethodGet,
			path:       "/",
			body:       nil, // No body for GET requests
			statusCode: http.StatusOK,
		},
		{
			name:        "invalid POST request to /validation with empty body",
			method:      http.MethodPost,
			path:        "/validation",
			body:        bytes.NewBuffer([]byte(``)),
			statusCode:  http.StatusBadRequest,
			contentType: echo.MIMEApplicationJSON,
		},
		{
			name:        "valid POST request to /validation",
			method:      http.MethodPost,
			path:        "/validation",
			body:        bytes.NewBuffer([]byte(`{"username": "test"}`)),
			statusCode:  http.StatusOK,
			contentType: echo.MIMEApplicationJSON,
		},
	}

	e := echo.New()

	e.Any("/", func(c echo.Context) error {
		return c.JSON(http.StatusOK, "ok")
	})

	e.Any("/validation", func(c echo.Context) error {
		return c.JSON(http.StatusOK, "ok")
	})

	e.Use(OpenAPIFromBytes(sampleOpenAPISpec))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request

			if tc.body == nil {
				req = httptest.NewRequest(tc.method, tc.path, http.NoBody)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, tc.body)
			}

			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			resp := httptest.NewRecorder()
			e.ServeHTTP(resp, req)

			assert.Equal(t, tc.statusCode, resp.Code)
		})
	}
}
