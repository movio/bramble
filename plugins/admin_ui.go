package plugins

import (
	"bytes"
	_ "embed"
	"errors"
	"html/template"
	log "log/slog"
	"net/http"
	"os"
	"sort"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"

	"github.com/movio/bramble"
)

func init() {
	bramble.RegisterPlugin(&AdminUIPlugin{})
}

// AdminUIPlugin serves a minimal administration interface.
type AdminUIPlugin struct {
	bramble.BasePlugin
	executableSchema *bramble.ExecutableSchema
	template         *template.Template
}

func (p *AdminUIPlugin) ID() string {
	return "admin-ui"
}

func (p *AdminUIPlugin) Init(s *bramble.ExecutableSchema) {
	tmpl := template.New("admin")
	_, err := tmpl.Parse(htmlTemplate)
	if err != nil {
		log.With("error", err).Error("unable to load admin UI page template")
		os.Exit(1)
	}

	p.template = tmpl
	p.executableSchema = s
}

func (p *AdminUIPlugin) SetupPrivateMux(mux *http.ServeMux) {
	mux.HandleFunc("/admin", p.handler)
}

type services []service

func (s services) Len() int {
	return len(s)
}

func (s services) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

func (s services) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type service struct {
	Name       string
	Version    string
	ServiceURL string
	Schema     string
	Status     string
}

type templateVariables struct {
	TestedSchema     string
	TestSchemaResult string
	TestSchemaError  string
	Services         services
}

func (p *AdminUIPlugin) handler(w http.ResponseWriter, r *http.Request) {
	var vars templateVariables

	if testSchema := r.FormValue("schema"); testSchema != "" {
		vars.TestedSchema = testSchema
		resultSchema, err := p.testSchema(testSchema)
		vars.TestSchemaResult = resultSchema
		if err != nil {
			vars.TestSchemaError = err.Error()
		}
	}

	for _, s := range p.executableSchema.Services {
		vars.Services = append(vars.Services, service{
			Name:       s.Name,
			Version:    s.Version,
			ServiceURL: s.ServiceURL,
			Schema:     s.SchemaSource,
			Status:     s.Status,
		})
	}

	sort.Sort(vars.Services)

	_ = p.template.Execute(w, vars)
}

func (p *AdminUIPlugin) testSchema(schemaStr string) (string, error) {
	schema, gqlErr := gqlparser.LoadSchema(&ast.Source{Input: schemaStr})
	if gqlErr != nil {
		return "", errors.New(gqlErr.Error())
	}

	if err := bramble.ValidateSchema(schema); err != nil {
		return "", err
	}

	schemas := []*ast.Schema{schema}
	for _, service := range p.executableSchema.Services {
		schemas = append(schemas, service.Schema)
	}

	result, err := bramble.MergeSchemas(schemas...)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	f := formatter.NewFormatter(&buf)
	f.FormatSchema(result)

	return buf.String(), nil
}

//go:embed admin_ui.html.template
var htmlTemplate string
