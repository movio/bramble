package plugins

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/movio/bramble"
)

func init() {
	bramble.RegisterPlugin(&PlaygroundPlugin{})
}

type PlaygroundPlugin struct {
	*bramble.BasePlugin
}

func (p *PlaygroundPlugin) ID() string {
	return "playground"
}

func (p *PlaygroundPlugin) SetupPublicMux(mux *http.ServeMux) {
	mux.HandleFunc("/playground", playground.Handler("Bramble Playground", "/query"))
}
