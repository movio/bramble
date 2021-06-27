# Plugins

## Admin UI

Admin UI provides a simple administration interface displaying the federated services.

```json
{
  "name": "admin-ui"
}
```

You access the Admin UI by visiting `http://localhost:<private-port>/admin` in your browser.

## CORS

Add `CORS` headers to queries.

```json
{
  "name": "cors",
  "config": {
    "allowed-origins": ["https://example.com"],
    "allowed-headers": ["X-Custom-Header"],
    "allow-credentials": true,
    "max-age": 3600,
    "debug": true
  }
}
```

## JWT Auth

The JWT auth plugin validates that the request contains a valid JWT and
provides roles support.

#### Public keys

Public keys can be provided through:

- JWKS endpoints
- Manually in the config

#### JWT

The plugin checks for the JWT in:

- The `Authorization` header: `Authorization: Bearer <JWT>`
- The `token` cookie

#### Roles

The JWT must contains a `role` claim with a valid role (as defined in the
config).

A role is a named set of permissions (as described in [access
control](access-control.md)).
When receiving a query with a valid JWT the permissions associated with the role will be added to the query.

!> **If a JWT is not present in the request, the request will proceed with the `public` role.**
So be sure to leave the `public` role empty is you do not want any unauthenticated access.

#### Configuration

```json
{
  "name": "auth-jwt",
  "config": {
    "JWKS": ["http://example.com/keys.jwks"],
    "public-keys": {
      "my-kid": "PUBLIC KEY"
    },
    "roles": {
      // example public role, allow only login mutation
      "public": {
        "mutation": {
          "auth": ["login"]
        }
      },
      // example internal role, allow all
      "internal": {
        "query": "*",
        "mutation": "*"
      }
    }
  }
}
```

## Limits

Set limits for response time and incoming requests size.

```json
{
  "name": "limits",
  "config": {
    "max-response-time": "10s",
    "max-request-bytes": 1000000
  }
}
```

## Playground

Exposes the GraphQL playground on `/playground`.

```json
{
  "name": "playground"
}
```

You access the GraphQL playground by visiting `http://localhost:<gateway-port>/playground` in your browser.

## Open Tracing (Jaeger)

The Jaeger plugin captures and sends traces to a Jaeger server.

Configuration is done through environment variables, see the [Jaeger
documentation](https://github.com/jaegertracing/jaeger-client-go#environment-variables)
for more information.
