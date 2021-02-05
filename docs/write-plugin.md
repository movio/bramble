# Writing a plugin

## Plugin interface

Plugins must implement the [`Plugin`](https://pkg.go.dev/github.com/movio/bramble/bramble#Plugin) interface. Optionaly they can derive from
the [`BasePlugin`](https://pkg.go.dev/github.com/movio/bramble/bramble#BasePlugin) implementation as to avoid redefining all methods.

```go
type MyPlugin struct {
    bramble.Plugin
}

func (p *MyPlugin) ID() string {
    return "my-plugin"
}
```

## Plugin registration

Plugins must register themselves using `RegisterPlugin`.
Once registered they can be enabled and configured through Bramble's configuration.

```go
func init() {
    bramble.RegisterPlugin(&MyPlugin{})
}
```

?> The `init` function can be defined multiple times in the same package.

## Compiling Bramble with custom Plugins

To build Bramble with custom plugins simply create your own `main.go` with an anonymous import of your plugins' package.

```go
package main

import (
	"github.com/movio/bramble"
	_ "github.com/movio/bramble/plugins"
	_ "github.com/your/custom/package"
)

func main() {
	bramble.Main()
}
```

## How to

### Configure the plugin

```go
type MyPluginConfig struct {
	// ...
}

type MyPlugin struct {
	config MyPluginConfig
}

func (p *MyPlugin) Configure(cfg *bramble.Config, data json.RawMessage) error {
	// data contains the raw "config" JSON for the plugin
	return json.Unmarshal(data, &p.config)
}
```

### Initialize the plugin

`Init` gives an opportunity to the plugin to access and store a pointer to
the `ExecutableSchema` (contains information about the schema, services...).

```go
func (p *MyPlugin) Init(s *bramble.ExecutableSchema) {
	// ...
}
```

### Register a new route

```go
func (p *MyPlugin) SetupPublicMux(mux *http.ServeMux) {
	mux.HandleFunc("/my-new-public-route", newRouteHandler)
}

func (p *MyPlugin) SetupPrivateMux(mux *http.ServeMux) {
	mux.HandleFunc("/my-new-private-route", newRouteHandler)
}

func newRouteHandler(w http.ResponseWriter, r *http.Request) {
	// ...
}
```

### Expose and federate a GraphQL endpoint

Plugins can also act as federated services. For this use `GraphqlQueryPath`
to return the private route used by your graphql endpoint.

```go
func (i *InternalsServicePlugin) GraphqlQueryPath() (bool, string) {
	return true, "my-graphql-endpoint"
}

func (i *InternalsServicePlugin) SetupPrivateMux(mux *http.ServeMux) {
	mux.Handle("/my-graphql-endpoint", myGraphqlHandler)
}
```

### Apply a middleware

```go
func (p *MyPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	// add a timeout to queries
	return http.TimeoutHandler(h, 1 * time.Second, "query timeout")
}
```
