package openapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

type TestHandler struct {
	*Handler
}

func (h *Handler) Root(c echo.Context) error {
	return h.Validate(c, http.StatusOK, echo.Map{"message": "welcome"})
}

func (h *Handler) Validation(c echo.Context) error {
	return h.Validate(c, http.StatusOK, echo.Map{"invalid": "welcome"})
}

func (h *Handler) NoContent(c echo.Context) error {
	return h.Validate(c, http.StatusNoContent, nil)
}

func (h *Handler) Text(c echo.Context) error {
	return h.ValidateWithContentType(c, http.StatusOK, echo.MIMETextPlain, "ok")
}

func (h *Handler) Bytes(c echo.Context) error {
	return h.ValidateWithContentType(c, http.StatusOK, echo.MIMETextPlain, []byte(`ok`))
}

func (h *Handler) InvalidType(c echo.Context) error {
	return h.ValidateWithContentType(c, http.StatusOK, echo.MIMETextPlain, []string{})
}

func TestHandler_Validate_Defaults(t *testing.T) {
	e := echo.New()

	h := TestHandler{NewHandler()}

	e.Add(http.MethodGet, "/", h.Root)

	e.Use(OpenAPI("./fixtures/openapi.yaml"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	e.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestHandler_ValidateWithConfig_Defaults(t *testing.T) {
	e := echo.New()

	h := TestHandler{NewHandlerWithConfig(HandlerConfig{})}

	e.Add(http.MethodGet, "/", h.Root)

	e.Use(OpenAPI("./fixtures/openapi.yaml"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	e.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestHandler_Validate_Error(t *testing.T) {
	e := echo.New()

	h := TestHandler{NewHandler()}

	e.Add(http.MethodGet, "/", h.Validation)

	e.Use(OpenAPI("./fixtures/openapi.yaml"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	e.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusInternalServerError, resp.Code)
}

func TestHandler_Validate_No_Content(t *testing.T) {
	e := echo.New()

	h := TestHandler{NewHandler()}

	e.Add(http.MethodPost, "/no-content", h.NoContent)

	e.Use(OpenAPI("./fixtures/openapi.yaml"))

	req := httptest.NewRequest(http.MethodPost, "/no-content", nil)
	resp := httptest.NewRecorder()

	e.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusNoContent, resp.Code)
}

func TestHandler_ValidateWithContentType_Text_Plain(t *testing.T) {
	h := TestHandler{NewHandler()}

	testCases := []struct {
		name       string
		handler    echo.HandlerFunc
		statusCode int
	}{
		{"string", h.Text, http.StatusOK},
		{"bytes", h.Bytes, http.StatusOK},
		{"invalid type", h.InvalidType, http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			e.Add(http.MethodGet, "/text", tc.handler)

			e.Use(OpenAPI("./fixtures/openapi.yaml"))

			req := httptest.NewRequest(http.MethodGet, "/text", nil)
			resp := httptest.NewRecorder()

			e.ServeHTTP(resp, req)

			if tc.statusCode == http.StatusOK {
				assert.Equal(t, http.StatusOK, resp.Code)
				assert.Equal(t, echo.MIMETextPlain, resp.Header().Get("Content-Type"))
			} else {
				assert.Equal(t, http.StatusInternalServerError, resp.Code)
			}
		})
	}
}
