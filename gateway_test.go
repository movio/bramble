package bramble

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string
		}
		json.NewDecoder(r.Body).Decode(&req)

		if strings.Contains(req.Query, "service") {
			// initial query to get schema
			schema := `type Service {
				name: String!
				version: String!
				schema: String!
			}

			type Query {
				test: String
				service: Service!
			}`
			encodedSchema, _ := json.Marshal(schema)
			fmt.Fprintf(w, `{
				"data": {
					"service": {
						"schema": %s,
						"version": "1.0",
						"name": "test-service"
					}
				}
			}`, string(encodedSchema))
			assert.Equal(t, "Bramble/dev (update)", r.Header.Get("User-Agent"))
		} else {
			w.Write([]byte(`{ "data": { "test": "Hello" }}`))
			assert.Equal(t, "Bramble/dev (query)", r.Header.Get("User-Agent"))
		}
	}))
	client := NewClient(WithUserAgent(GenerateUserAgent("query")))
	executableSchema := NewExecutableSchema(nil, 50, client, NewService(server.URL))
	err := executableSchema.UpdateSchema(true)
	require.NoError(t, err)
	gtw := NewGateway(executableSchema, []Plugin{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(`
	{
		"query": "query { test }"
	}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json; charset=utf-8")

	gtw.Router(&Config{}).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data": { "test": "Hello" }}`, rec.Body.String())
}

func TestRequestJSONBodyLogging(t *testing.T) {
	server := NewGateway(NewExecutableSchema(nil, 50, nil), nil).Router(&Config{})

	body := map[string]interface{}{
		"foo": "bar",
	}
	jr, jw := io.Pipe()
	go func() {
		enc := json.NewEncoder(jw)
		enc.Encode(body)
		jw.Close()
	}()
	defer jr.Close()

	req := httptest.NewRequest("POST", "/query", jr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	obj := collectLogEvent(t, func() {
		server.ServeHTTP(w, req)
	})
	resp := w.Result()

	assert.NotNil(t, obj)
	assert.Equal(t, float64(resp.StatusCode), obj["response.status"])
	assert.Equal(t, "application/json", obj["request.content-type"])
	assert.IsType(t, make(map[string]interface{}), obj["request.body"])
	assert.Equal(t, body, obj["request.body"])
}

func TestRequestInvalidJSONBodyLogging(t *testing.T) {
	server := NewGateway(nil, nil).Router(&Config{})

	body := `{ "invalid": "json`
	jr, jw := io.Pipe()
	go func() {
		jw.Write([]byte(body))
		jw.Close()
	}()
	defer jr.Close()

	req := httptest.NewRequest("POST", "/query", jr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	obj := collectLogEvent(t, func() {
		server.ServeHTTP(w, req)
	})
	w.Result()

	assert.NotNil(t, obj)
	assert.Equal(t, "application/json", obj["request.content-type"])
	assert.IsType(t, "string", obj["request.body"])
	assert.Equal(t, body, obj["request.body"])
	assert.Equal(t, "unexpected end of JSON input", obj["request.error"])
}

func TestRequestTextBodyLogging(t *testing.T) {
	server := NewGateway(nil, nil).Router(&Config{})

	body := `the request body`
	jr, jw := io.Pipe()
	go func() {
		jw.Write([]byte(body))
		jw.Close()
	}()
	defer jr.Close()

	req := httptest.NewRequest("POST", "/query", jr)
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	obj := collectLogEvent(t, func() {
		server.ServeHTTP(w, req)
	})
	w.Result()

	assert.NotNil(t, obj)
	assert.Equal(t, "text/plain", obj["request.content-type"])
	assert.IsType(t, "string", obj["request.body"])
	assert.Equal(t, body, obj["request.body"])
	assert.Equal(t, nil, obj["request.error"])
}

func TestDebugMiddleware(t *testing.T) {
	t.Run("without debug header", func(t *testing.T) {
		called := false
		req := httptest.NewRequest("POST", "/", nil)
		h := func(w http.ResponseWriter, r *http.Request) {
			called = true
			info, ok := r.Context().Value(DebugKey).(DebugInfo)
			assert.True(t, ok, "context should include debugInfo")
			assert.False(t, info.Variables)
			assert.False(t, info.Query)
			assert.False(t, info.Plan)
			w.WriteHeader(http.StatusOK)
		}
		server := debugMiddleware(http.HandlerFunc(h))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		assert.True(t, called, "handler not called")
	})
	for header, expected := range map[string]DebugInfo{
		"all": {
			Variables: true,
			Query:     true,
			Plan:      true,
		},
		"query": {
			Query: true,
		},
		"variables": {
			Variables: true,
		},
		"plan": {
			Plan: true,
		},
		"query plan": {
			Query: true,
			Plan:  true,
		},
	} {
		t.Run("with debug header value all", func(t *testing.T) {
			called := false
			req := httptest.NewRequest("POST", "/", nil)
			req.Header.Set(debugHeader, header)
			h := func(w http.ResponseWriter, r *http.Request) {
				called = true
				info, ok := r.Context().Value(DebugKey).(DebugInfo)
				assert.True(t, ok, "context should include debugInfo")
				assert.Equal(t, expected.Variables, info.Variables)
				assert.Equal(t, expected.Query, info.Query)
				assert.Equal(t, expected.Plan, info.Plan)
				w.WriteHeader(http.StatusOK)
			}
			server := debugMiddleware(http.HandlerFunc(h))
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)
			assert.True(t, called, "handler not called")
		})
	}
}
