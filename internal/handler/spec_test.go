package handler_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/stretchr/testify/require"
)

// specPath is the OpenAPI document relative to this package.
const specPath = "../../api/openapi.yaml"

// specRouter loads the OpenAPI document once and returns a router that
// finds the documented operation for a request.
var specRouter = sync.OnceValues(func() (routers.Router, error) {
	loader := openapi3.NewLoader()

	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, err
	}

	if err := doc.Validate(loader.Context); err != nil {
		return nil, err
	}

	// The document lists http://localhost:8080 as its server. Test requests
	// carry another host, so the router must match on the path alone.
	doc.Servers = nil

	return gorillamux.NewRouter(doc)
})

// requireMatchesSpec fails the test unless the exchange is described by the
// OpenAPI document: the operation exists, the status code is documented for
// it and the body conforms to the schema of that response.
//
// This is what keeps api/openapi.yaml from drifting away from the handlers:
// a response the document does not describe breaks the build.
func requireMatchesSpec(t *testing.T, request *http.Request, recorder *httptest.ResponseRecorder) {
	t.Helper()

	router, err := specRouter()
	require.NoError(t, err, "load %s", specPath)

	route, pathParams, err := router.FindRoute(request)
	require.NoError(t, err, "%s %s is not described in %s", request.Method, request.URL.Path, specPath)

	err = openapi3filter.ValidateResponse(t.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    request,
			PathParams: pathParams,
			Route:      route,
		},
		Status: recorder.Code,
		Header: recorder.Header(),
		Body:   io.NopCloser(bytes.NewReader(recorder.Body.Bytes())),
		Options: &openapi3filter.Options{
			// Without this an undocumented status code would pass.
			IncludeResponseStatus: true,
		},
	})
	require.NoError(t, err, "response to %s %s does not match %s", request.Method, request.URL.Path, specPath)
}
