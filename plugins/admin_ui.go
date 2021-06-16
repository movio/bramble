package plugins

import (
	"bytes"
	"errors"
	"net/http"
	"sort"
	"text/template"

	log "github.com/sirupsen/logrus"
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
		log.WithError(err).Fatal("unable to load admin UI page template")
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

const htmlTemplate = `
<html>

<head>
    <title>Admin</title>
    <style>
        body {
            font-family: arial, serif;
            font-size: 0.9em;
        }

        h2 {
            margin: 20px;
            text-align: center;
            font-weight: normal;
        }

        ul {
            text-align: center;
            margin: auto;
        }

        li.service {
            text-align: left;
            list-style-type: none;
            display: inline-block;
            box-shadow: 0 4px 8px 0 rgba(0, 0, 0, 0.1), 0 6px 20px 0 rgba(0, 0, 0, 0.1);
            margin: 20px;
            position: relative;
            vertical-align: top;
            width: 500px;
        }

        li.service .header {
            margin: 0;
            padding: 10px 20px;
            background: #2c5282;
            color: #f0f3f5;
        }

        .status-ok .header,
        .status-ok .title {
            border-left: 5px solid #7ebd6f;
        }

        .status-error .header,
        .status-error .title {
            border-left: 5px solid #bf4e4e;
        }

        .header h3 {
            margin: 0;
            margin-bottom: 10px;
        }

        .header .version {
            position: absolute;
            right: 10px;
            top: 20px;
        }

        .header .url {
            width: 460px;
            word-wrap: break-word;
            font-size: 0.9em;
        }

        .collapsible {
            display: block;
            background: #f5f2f0;
            padding: 0;
        }

        input[type="checkbox"] {
            display: none;
        }

        .collapsed {
            max-height: 0px;
            width: 100%;
            overflow: hidden;
            border-left: 5px solid silver;
        }

        .collapsible input:checked~.collapsed {
            max-height: 400px;
            overflow: scroll;
            padding: 0;
            margin: 0;
        }

        .collapsible pre {
            padding: 10px;
        }

        .title {
            display: inline-block;
            background: #2a4365;
            color: white;
            width: 495px;
            font-size: 0.9em;
        }

        .title span {
            display: inline-block;
            padding: 10px 20px;
        }

        form {
            text-align: center;
        }

        input[type="submit"] {
            font-size: 1.3em;
            margin: 20px;
        }

        textarea {
            display: block;
            width: 50%;
            min-height: 300px;
            margin: auto;
            font-size: 1.3em;
            padding: 15px;
            line-height: 1.5em;
        }

        div#test-result {
            margin: 20px auto;
            width: 50%;
        }

        p#test-result-error {
            margin: 15px 0;
            font-size: 1.3em;
        }

        .success {
            color: #56a861;
        }

        .error {
            color: #913533;
            font-weight: bold;
        }

        h2 {
            margin-top: 50px;
        }
    </style>
</head>

<body>
    <h2>Aggregated services</h2>
    <ul>
        {{range .Services}}
        <li class="service {{if (eq .Status "OK")}} {{"status-ok"}} {{else}} {{"status-error"}} {{end}}">
            <div class="header">
                <h3>{{.Name}}</h3>
                <div class="version">{{.Version}}</div>
                <div class="url">{{.ServiceURL}}</div>
                <div class="status">{{.Status}}</div>
            </div>
            <label class="collapsible">
                <input type="checkbox" />
                <div class="title"><span>+ Schema</span></div>
                <div class="collapsed">
                    <pre>{{.Schema}}</pre>
                </div>
            </label>
        </li>
        {{end}}
    </ul>
    <h2>Test schema merge</h2>
    {{if ne .TestedSchema "" }}
    <div id="test-result">
        {{if eq .TestSchemaError ""}}
        <p id="test-result-error" class="success">
            Schema merged successfully!
        </p>
        <label class="collapsible">
            <input type="checkbox" />
            <div class="title"><span>+ Result schema</span></div>
            <div class="collapsed">
                <pre>{{.TestSchemaResult}}</pre>
            </div>
        </label>
        {{else}}
        <p id="test-result-error" class="error">
            {{.TestSchemaError}}
        </p>
        {{end}}
    </div>
    {{end}}
    <form method="POST">
        <textarea name="schema"
            placeholder="Paste a schema here to check if it would merge successfully with the existing schema">{{.TestedSchema}}</textarea>
        <input type="submit" value="Check" />
    </form>
</body>

</html>
`
