package bramble

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphqlClient(t *testing.T) {
	t.Run("basic request", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{
				"data": {
					"root": {
						"test": "value"
					}
				}
			}`))
		}))

		c := NewClient()
		var res struct {
			Root struct {
				Test string
			}
		}

		err := c.Request(context.Background(), srv.URL, &Request{}, &res)
		assert.NoError(t, err)
		assert.Equal(t, "value", res.Root.Test)
	})

	t.Run("with user agent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "My User Agent", r.Header.Get("User-Agent"))
		}))

		c := NewClient(WithUserAgent("My User Agent"))
		var res interface{}
		_ = c.Request(context.Background(), srv.URL, &Request{}, &res)
	})

	t.Run("with max response size", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{ "data": "long response" }`))
		}))

		c := NewClient(WithMaxResponseSize(1))
		var res interface{}
		err := c.Request(context.Background(), srv.URL, &Request{}, &res)
		require.Error(t, err)
		assert.Equal(t, "response exceeded maximum size of 1 bytes", err.Error())
	})
}
