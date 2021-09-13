package plugins

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/movio/bramble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/square/go-jose.v2"
)

const testPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEpgIBAAKCAQEAwku141JPd9mYwYBCTygPvuIko2QDiDUj0sDaHRGwWxspGKsn
wisEkVlL6R9m7I1G43jbgp3VaQLZRmNB+WlNhXVVWm4JbwCFWSdvE9aBkjEVfucI
d3U5/dmLpOmtsi+IRcGIN960ks1yoJqo6pkfli+r9xLyProbIA5N0zpEegYfilpQ
/bqIsxGcmcSqkzzT7u3/lZDVbn0+3Tkq0FZ0p0iyWSAWba0DzesGGzUwknJJZ+Lw
6aNRSBqgvmVia38YTyOCRxcaaTFHahc3hyNN8X4GIhO2wN6EFuConznR71X+zh6Q
2jj/Ci0OUzcBA/vAmxo81qq/hw+micF2xR3MQQIDAQABAoIBAQCb4qyjHvX9XYrO
rT4GTkkbyErHAMZIsQH15J7atcd9wTPew+uZQHRgvXlHJ9enMM5QUTYk/Mctgoia
jaZwGkmFKxd4/1H4Sj2yww2+p9q7VUA+2dQUK+yEO9drT8T5cmNuPBEzai4MnmM6
cfvWhVYvZD4fdIcBRsXemTtdnqE0GHFNM3bgAF26fqJDYZceR/aAfGMP+jYA1tXa
2lkCnQyy7W9Go/LjN0XqjeCzcN3HWSCE4ONmmPEUOm4fMrB8tdjoFDOKWsviOQXl
/ixAUqic5E1c2/ff3G+iJ1C98bLpdmc6TwOomczelQ+YqFfBARwexu38a2AgTOZ0
NRdw+6nxAoGBAOnXLkpk2GB2lOt3pGWKTxHB/mms6UUlMh9078ZyoSPYANyVggUC
H81DUPkCL2R3tu9lxiAN5Fj7qXB7KYxqZMsL23jF3hGi1RbK0L00X6+fpUXBkv+b
DdGAAM7bgpNo0Ww1sTh1VX1Kf5rdNvic9E46BqAb72xo4uoraKHkJ3WtAoGBANS1
Ms/1aBiJFFGArS+vtHcGeU7ffYhL6x+iAj3p1Vrb11Vd8ZjvIvkUflUg2AaYfsG0
yoTrUvb399SDofaAM+1ylIBnqltCiUX3ZKcX338ujJbo3jW/sXPw8fosjkemCUFg
pT9j86Et12sMmm+kHzcPTVJjSxqVl/R+gIDV07tlAoGBAKVl2U0vhUi9t1nRt0tH
B+RkleIDNr/8rjZHzO1N2SJ0Py/G5D9MoFfcfGKUpBbpAlDUaM31ZYV3BAMWam3y
NzbTPTpwokFRLm2/qOObLu8W+ZycbbAz6RM8+dVWuEYxxqdGVwK7I2vKjPVp8N7q
jXbjXhpTiAbjLVU6vPh9W1fFAoGBAIyPoSBjn4J3M4IYclnM1ojBMnC4p4/l+15Q
BQM8/syn8khraDgT7xyCOmmu5pKVO05uVlY32/9wJcm9os3uMmJ7ET85Qg5Ejco6
jb0NvZeh/y3KfO0v2+guFPmpb+xRAFS/tPOK7XhZfr0y+utDnY0ZA5OqIftTV7Mt
1WVN6DkxAoGBANo/rsM9en4BYRu8KFGXlY1niUwKZuJYdk31g12xxVpj8PLU92Pr
OFbJL6nupeyfOcEGKy9pyQ+aeIC9ZSHKFQKDLh8Gg8/MopsBGcUBPE98FRparvrX
rq/PmqBaJYfz1JapR/Qt9ecFxwSjqZtfjWaBBhvkYDU1FfOHmZPWDxJi
-----END RSA PRIVATE KEY-----`

const testPublicKey = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwku141JPd9mYwYBCTygP
vuIko2QDiDUj0sDaHRGwWxspGKsnwisEkVlL6R9m7I1G43jbgp3VaQLZRmNB+WlN
hXVVWm4JbwCFWSdvE9aBkjEVfucId3U5/dmLpOmtsi+IRcGIN960ks1yoJqo6pkf
li+r9xLyProbIA5N0zpEegYfilpQ/bqIsxGcmcSqkzzT7u3/lZDVbn0+3Tkq0FZ0
p0iyWSAWba0DzesGGzUwknJJZ+Lw6aNRSBqgvmVia38YTyOCRxcaaTFHahc3hyNN
8X4GIhO2wN6EFuConznR71X+zh6Q2jj/Ci0OUzcBA/vAmxo81qq/hw+micF2xR3M
QQIDAQAB
-----END PUBLIC KEY-----`

func TestJWTPlugin(t *testing.T) {
	manualKeyProvider, err := NewManualSigningKeysProvider(map[string]string{"": testPublicKey})
	require.NoError(t, err)
	keyProviders := []SigningKeyProvider{manualKeyProvider}
	t.Run("valid token", func(t *testing.T) {
		basicRole := bramble.OperationPermissions{
			AllowedRootQueryFields: bramble.AllowedFields{
				AllowAll: true,
			},
		}
		privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(testPrivateKey))
		require.NoError(t, err)

		token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, &Claims{
			Role: "basic_role",
			StandardClaims: jwt.StandardClaims{
				Audience: "test-audience",
				Id:       "test-id",
				Issuer:   "test-issuer",
				Subject:  "test-subject",
			},
		}).SignedString(privateKey)
		require.NoError(t, err)

		mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := bramble.GetPermissionsFromContext(r.Context())
			assert.True(t, ok)
			assert.Equal(t, basicRole, role)
			w.WriteHeader(http.StatusTeapot)
		})

		jwtPlugin := NewJWTPlugin(keyProviders, map[string]bramble.OperationPermissions{
			"basic_role": basicRole,
		})

		handler := jwtPlugin.ApplyMiddlewarePublicMux(mockHandler)
		req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader("{}"))
		req.Header.Add("authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusTeapot, rr.Result().StatusCode)
	})

	t.Run("expired token", func(t *testing.T) {
		privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(testPrivateKey))
		require.NoError(t, err)

		token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, &Claims{
			StandardClaims: jwt.StandardClaims{
				ExpiresAt: time.Now().Add(-1 * time.Second).Unix(),
			},
			Role: "basic_role",
		}).SignedString(privateKey)
		require.NoError(t, err)

		mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		jwtPlugin := NewJWTPlugin(keyProviders, nil)

		handler := jwtPlugin.ApplyMiddlewarePublicMux(mockHandler)
		req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader("{}"))
		req.Header.Add("authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Result().StatusCode)
	})

	t.Run("invalid kid", func(t *testing.T) {
		privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(testPrivateKey))
		require.NoError(t, err)

		token := jwt.NewWithClaims(jwt.SigningMethodRS256, &Claims{
			Role: "basic_role",
		})
		token.Header["kid"] = "invalid_kid"

		tokenStr, err := token.SignedString(privateKey)
		require.NoError(t, err)

		mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		jwtPlugin := NewJWTPlugin(keyProviders, nil)

		handler := jwtPlugin.ApplyMiddlewarePublicMux(mockHandler)
		req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader("{}"))
		req.Header.Add("authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Result().StatusCode)
	})

	t.Run("JWKS", func(t *testing.T) {
		privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(testPrivateKey))
		require.NoError(t, err)

		jwksHandler := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var jwks jose.JSONWebKeySet
			jwks.Keys = append(jwks.Keys, jose.JSONWebKey{
				Key:       &privateKey.PublicKey,
				KeyID:     "test-key-id",
				Algorithm: string(jose.RS256),
			})
			_ = json.NewEncoder(w).Encode(jwks)
		}))

		jwtPlugin := NewJWTPlugin(nil, nil)
		err = jwtPlugin.Configure(&bramble.Config{}, json.RawMessage(fmt.Sprintf(`{
			"jwks": [%q],
			"roles": {
				"basic_role": {
					"query": "*"
				}
			}
		}`, jwksHandler.URL)))
		require.NoError(t, err)

		token := jwt.NewWithClaims(jwt.SigningMethodRS256, &Claims{
			Role: "basic_role",
			StandardClaims: jwt.StandardClaims{
				Audience: "test-audience",
				Id:       "test-id",
				Issuer:   "test-issuer",
				Subject:  "test-subject",
			},
		})
		token.Header["kid"] = "test-key-id"
		tokenStr, err := token.SignedString(privateKey)
		require.NoError(t, err)

		mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})

		handler := jwtPlugin.ApplyMiddlewarePublicMux(mockHandler)
		req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader("{}"))
		req.Header.Add("authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusTeapot, rr.Result().StatusCode)
	})
}
