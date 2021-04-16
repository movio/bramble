package plugins

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/movio/bramble"
	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestAdminUI(t *testing.T) {
	plugin := &AdminUIPlugin{}
	es := &bramble.ExecutableSchema{
		Services: map[string]*bramble.Service{
			"svc-a": {
				Schema: gqlparser.MustLoadSchema(&ast.Source{Input: ``}),
			},
			"svc-b": {
				Schema: gqlparser.MustLoadSchema(&ast.Source{Input: ``}),
			},
		},
	}
	plugin.Init(es)
	m := http.NewServeMux()
	plugin.SetupPrivateMux(m)

	t.Run("test valid schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin", nil)
		req.Form = url.Values{
			"schema": []string{`
			type Service {
				name: String!
				version: String!
				schema: String!
			}
			type Query { service: Service! }`},
		}
		rr := httptest.NewRecorder()
		m.ServeHTTP(rr, req)

		assert.Contains(t, rr.Body.String(), "Schema merged successfully")
	})

	t.Run("test invalid schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin", nil)
		req.Form = url.Values{
			"schema": []string{`type Query { foo: Bar! }`},
		}
		rr := httptest.NewRecorder()
		m.ServeHTTP(rr, req)

		assert.NotContains(t, rr.Body.String(), "Schema merged successfully")
	})
}
