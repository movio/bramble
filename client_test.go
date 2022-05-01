package bramble

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

	t.Run("without keep-alive", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "close", r.Header.Get("Connection"))
			w.Write([]byte(`{
				"data": {
					"root": {
						"test": "value"
					}
				}
			}`))
		}))

		c := NewClientWithoutKeepAlive()
		err := c.Request(context.Background(), srv.URL, &Request{}, nil)
		assert.NoError(t, err)
	})

	t.Run("with http client", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("test_cookie")
			require.NoError(t, err)
			assert.Equal(t, "test_value", cookie.Value)
		}))

		jar, err := cookiejar.New(nil)
		require.NoError(t, err)

		serverURL, err := url.Parse(srv.URL)
		require.NoError(t, err)

		jar.SetCookies(serverURL, []*http.Cookie{
			{

				Name:  "test_cookie",
				Value: "test_value",
			},
		})

		httpClient := &http.Client{Jar: jar}
		c := NewClient(WithHTTPClient(httpClient))
		var res interface{}
		_ = c.Request(context.Background(), srv.URL, &Request{}, &res)
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
