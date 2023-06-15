package plugins

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/golang-jwt/jwt/v4"
	"github.com/golang-jwt/jwt/v4/request"
	"github.com/movio/bramble"
	log "github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2"
)

func init() {
	bramble.RegisterPlugin(NewJWTPlugin(nil, nil))
}

func NewJWTPlugin(keyProviders []SigningKeyProvider, roles map[string]bramble.OperationPermissions) *JWTPlugin {
	publicKeys := make(map[string]*rsa.PublicKey)
	for _, p := range keyProviders {
		keys, err := p.Keys()
		if err != nil {
			log.WithError(err).Fatalf("couldn't get signing keys for provider %q", p.Name())
		}
		for id, k := range keys {
			publicKeys[id] = k
		}
	}

	return &JWTPlugin{
		publicKeys: publicKeys,
		config: JWTPluginConfig{
			Roles: roles,
		},
		jwtExtractor: request.MultiExtractor{
			request.AuthorizationHeaderExtractor,
			cookieTokenExtractor{cookieName: "token"},
		},
	}
}

// JWTPlugin validates that requests contains a valid JWT access token and add
// the necessary permissions and information to the context
type JWTPlugin struct {
	config       JWTPluginConfig
	keyProviders []SigningKeyProvider
	publicKeys   map[string]*rsa.PublicKey
	jwtExtractor request.Extractor

	bramble.BasePlugin
}

type JWTPluginConfig struct {
	// List of JWKS endpoints
	JWKS []WellKnownKeyProvider `json:"jwks"`
	// Map of kid -> public key (RSA, PEM format)
	PublicKeys map[string]string                       `json:"public-keys"`
	Roles      map[string]bramble.OperationPermissions `json:"roles"`
}

type SigningKeyProvider interface {
	Name() string
	Keys() (map[string]*rsa.PublicKey, error)
}

func (p *JWTPlugin) ID() string {
	return "auth-jwt"
}

func (p *JWTPlugin) Configure(cfg *bramble.Config, data json.RawMessage) error {
	err := json.Unmarshal(data, &p.config)
	if err != nil {
		return err
	}

	for _, k := range p.config.JWKS {
		p.keyProviders = append(p.keyProviders, &k)
	}

	if len(p.config.PublicKeys) > 0 {
		provider, err := NewManualSigningKeysProvider(p.config.PublicKeys)
		if err != nil {
			return fmt.Errorf("error creating manual keys provider: %w", err)
		}
		p.keyProviders = append(p.keyProviders, provider)
	}

	p.publicKeys = make(map[string]*rsa.PublicKey)
	for _, kp := range p.keyProviders {
		keys, err := kp.Keys()
		if err != nil {
			return fmt.Errorf("couldn't get signing keys for provider %q: %w", kp.Name(), err)
		}
		for id, k := range keys {
			p.publicKeys[id] = k
		}
	}

	return nil
}

type Claims struct {
	jwt.StandardClaims
	Role string
}

func (p *JWTPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		tokenStr, err := p.jwtExtractor.ExtractToken(r)
		if err != nil {
			// unauthenticated request, must use "public_role"
			log.Info("unauthenticated request")
			r = r.WithContext(bramble.AddPermissionsToContext(r.Context(), p.config.Roles["public_role"]))
			h.ServeHTTP(rw, r)
			return
		}

		var claims Claims
		_, err = jwt.ParseWithClaims(tokenStr, &claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}

			keyID, _ := token.Header["kid"].(string)
			if key, ok := p.publicKeys[keyID]; ok {
				return key, nil
			}

			return nil, fmt.Errorf("could not find key for kid %q", keyID)
		})
		if err != nil {
			log.WithError(err).Info("invalid token")
			rw.WriteHeader(http.StatusUnauthorized)
			writeGraphqlError(rw, "invalid token")
			return
		}

		role, ok := p.config.Roles[claims.Role]
		if !ok {
			log.WithField("role", claims.Role).Info("invalid role")
			rw.WriteHeader(http.StatusUnauthorized)
			writeGraphqlError(rw, "invalid role")
			return
		}

		bramble.AddFields(r.Context(), bramble.EventFields{
			"role":    claims.Role,
			"subject": claims.Subject,
		})

		ctx := r.Context()
		ctx = bramble.AddPermissionsToContext(ctx, role)
		ctx = addStandardJWTClaimsToOutgoingRequest(ctx, claims.StandardClaims)
		ctx = bramble.AddOutgoingRequestsHeaderToContext(ctx, "JWT-Claim-Role", claims.Role)
		h.ServeHTTP(rw, r.WithContext(ctx))
	})
}

func addStandardJWTClaimsToOutgoingRequest(ctx context.Context, claims jwt.StandardClaims) context.Context {
	if claims.Audience != "" {
		ctx = bramble.AddOutgoingRequestsHeaderToContext(ctx, "JWT-Claim-Audience", claims.Audience)
	}
	if claims.Id != "" {
		ctx = bramble.AddOutgoingRequestsHeaderToContext(ctx, "JWT-Claim-ID", claims.Id)
	}
	if claims.Issuer != "" {
		ctx = bramble.AddOutgoingRequestsHeaderToContext(ctx, "JWT-Claim-Issuer", claims.Issuer)
	}
	if claims.Subject != "" {
		ctx = bramble.AddOutgoingRequestsHeaderToContext(ctx, "JWT-Claim-Subject", claims.Subject)
	}
	return ctx
}

func writeGraphqlError(w io.Writer, message string) {
	json.NewEncoder(w).Encode(bramble.Response{Errors: bramble.GraphqlErrors{{Message: message}}})
}

// cookieTokenExtractor extracts a JWT token from the "token" cookie
type cookieTokenExtractor struct {
	cookieName string
}

func (c cookieTokenExtractor) ExtractToken(r *http.Request) (string, error) {
	cookie, err := r.Cookie(c.cookieName)
	if err != nil {
		return "", request.ErrNoTokenInRequest
	}
	return cookie.Value, nil
}

type ManualSigningKeysProvider struct {
	keys map[string]*rsa.PublicKey
}

func NewManualSigningKeysProvider(keys map[string]string) (*ManualSigningKeysProvider, error) {
	parsedKeys := make(map[string]*rsa.PublicKey)

	for kid, key := range keys {
		publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(key))
		if err != nil {
			return nil, err
		}
		parsedKeys[kid] = publicKey
	}

	return &ManualSigningKeysProvider{
		keys: parsedKeys,
	}, nil
}

func (m *ManualSigningKeysProvider) Name() string {
	return "manual"
}

func (m *ManualSigningKeysProvider) Keys() (map[string]*rsa.PublicKey, error) {
	return m.keys, nil
}

type WellKnownKeyProvider struct {
	url string
}

func NewWellKnownKeyProvider(url string) *WellKnownKeyProvider {
	return &WellKnownKeyProvider{
		url: url,
	}
}

func (w *WellKnownKeyProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.url)
}

func (w *WellKnownKeyProvider) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &w.url)
}

func (w *WellKnownKeyProvider) Name() string {
	return fmt.Sprintf("well-known, url: %q", w.url)
}

func (w *WellKnownKeyProvider) Keys() (map[string]*rsa.PublicKey, error) {
	resp, err := http.Get(w.url)
	if err != nil {
		return nil, fmt.Errorf("error requesting URL: %w", err)
	}
	defer resp.Body.Close()

	var s jose.JSONWebKeySet
	err = json.NewDecoder(resp.Body).Decode(&s)
	if err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	res := make(map[string]*rsa.PublicKey)
	for _, k := range s.Keys {
		rsaKey, ok := k.Key.(*rsa.PublicKey)
		if !ok {
			continue
		}

		res[k.KeyID] = rsaKey
	}

	return res, nil
}
