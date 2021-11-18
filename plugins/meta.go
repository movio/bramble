package plugins

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/movio/bramble"
	"github.com/vektah/gqlparser/v2/ast"
)

func init() {
	bramble.RegisterPlugin(NewMetaPlugin())
}

var metaPluginSchema = `
directive @namespace on OBJECT
directive @boundary on OBJECT | FIELD_DEFINITION
type Service {
	name: String!
	version: String!
	schema: String!
}
type BrambleService @boundary {
	id: ID!
	name: String!
	version: String!
	schema: String!
	status: String!
	serviceUrl: String!
}
type BrambleFieldArgument {
	name: String!
	type: String!
}
type BrambleField @boundary {
	id: ID!
	name: String!
	type: String!
	service: String!
	arguments: [BrambleFieldArgument!]!
	description: String
}
type BrambleEnumValue {
	name: String!
	description: String
}
type BrambleType @boundary {
	id: ID!
	kind: String!
	name: String!
	directives: [String!]!
	fields: [BrambleField!]!
	enumValues: [BrambleEnumValue!]!
	description: String
}
type BrambleSchema {
	types: [BrambleType!]!
}
type BrambleMetaQuery @namespace {
	services: [BrambleService!]!
	schema: BrambleSchema!
	field(id: ID!): BrambleField
}
type Query {
	service: Service!
	meta: BrambleMetaQuery!
	getField(id: ID!): BrambleField @boundary
	getType(id: ID!): BrambleType @boundary
	getService(id: ID!): BrambleService @boundary
}
`

type metaPluginResolver struct {
	Service struct {
		Name    string
		Version string
		Schema  string
	}
	executableSchema *bramble.ExecutableSchema
}

func newMetaPluginResolver() *metaPluginResolver {
	return &metaPluginResolver{
		Service: struct {
			Name    string
			Version string
			Schema  string
		}{
			Name:    "bramble-meta-plugin",
			Version: "latest",
			Schema:  metaPluginSchema,
		},
	}
}

func (r *metaPluginResolver) Meta() *metaPluginResolver {
	return r
}

type brambleArg struct {
	Name string
	Type string
}

type brambleField struct {
	ID          graphql.ID
	Name        string
	Type        string
	Service     string
	Description *string
	Arguments   []brambleArg
}

type brambleFields []brambleField

func (f brambleFields) Len() int {
	return len(f)
}

func (f brambleFields) Less(i, j int) bool {
	if f[i].Name == bramble.IdFieldName {
		return true
	}
	return f[i].Name < f[j].Name
}

func (f brambleFields) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}

type brambleEnumValue struct {
	Name        string
	Description *string
}

type brambleType struct {
	Kind        string
	Name        string
	Directives  []string
	Fields      []brambleField
	EnumValues  []brambleEnumValue
	Description *string
}

func (t brambleType) Id() graphql.ID {
	return graphql.ID(t.Name)
}

type brambleTypes []brambleType

func (t brambleTypes) Len() int {
	return len(t)
}

func (t brambleTypes) Less(i, j int) bool {
	return t[i].Name < t[j].Name
}

func (t brambleTypes) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

type brambleSchema struct {
	Types []brambleType
}

func (r *metaPluginResolver) Schema() (*brambleSchema, error) {
	schema := r.executableSchema.MergedSchema
	var types brambleTypes
	for name, def := range schema.Types {
		types = append(types, r.brambleType(name, def))
	}
	sort.Sort(types)
	return &brambleSchema{
		Types: types,
	}, nil
}

func kindToStr(k ast.DefinitionKind) string {
	if k == ast.InputObject {
		return "input"
	}
	if k == ast.Object {
		return "type"
	}
	return strings.ToLower(string(k))
}

func strToPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *metaPluginResolver) GetService(ctx context.Context, args struct{ ID graphql.ID }) *brambleService {
	for _, service := range r.executableSchema.Services {
		if service.Name == string(args.ID) {
			return &brambleService{
				Name:       service.Name,
				Version:    service.Version,
				Schema:     service.SchemaSource,
				Status:     service.Status,
				ServiceURL: service.ServiceURL,
			}
		}
	}
	return nil
}

func (p *metaPluginResolver) GetType(ctx context.Context, args struct{ ID graphql.ID }) (*brambleType, error) {
	typeName := string(args.ID)
	var typeDef *ast.Definition
	for _, def := range p.executableSchema.MergedSchema.Types {
		if def.Name == typeName {
			typeDef = def
			break
		}
	}
	if typeDef == nil {
		return nil, nil
	}
	result := p.brambleType(typeName, typeDef)
	return &result, nil
}

func (r *metaPluginResolver) brambleType(name string, def *ast.Definition) brambleType {
	var fields brambleFields
	for _, f := range def.Fields {
		if strings.HasPrefix(f.Name, "__") {
			continue
		}
		var svcName string
		if svcURL, err := r.executableSchema.Locations.URLFor(def.Name, "", f.Name); err == nil {
			svc := r.executableSchema.Services[svcURL]
			svcName = svc.Name
		}
		var args []brambleArg
		for _, a := range f.Arguments {
			args = append(args, brambleArg{
				Name: a.Name,
				Type: a.Type.String(),
			})
		}
		fields = append(fields, brambleField{
			ID:          graphql.ID(def.Name + "." + f.Name),
			Name:        f.Name,
			Type:        f.Type.String(),
			Service:     svcName,
			Description: strToPtr(f.Description),
			Arguments:   args,
		})
	}
	sort.Sort(fields)
	var enum []brambleEnumValue
	for _, v := range def.EnumValues {
		enum = append(enum, brambleEnumValue{
			Name:        v.Name,
			Description: strToPtr(v.Description),
		})
	}
	var directives []string
	for _, d := range def.Directives {
		directives = append(directives, d.Name)
	}
	return brambleType{
		Kind:        kindToStr(def.Kind),
		Name:        name,
		Directives:  directives,
		Fields:      fields,
		Description: strToPtr(def.Description),
		EnumValues:  enum,
	}
}

func (p *metaPluginResolver) Field(ctx context.Context, args struct{ ID graphql.ID }) (*brambleField, error) {
	return p.GetField(ctx, args)
}

func (p *metaPluginResolver) GetField(ctx context.Context, args struct{ ID graphql.ID }) (*brambleField, error) {
	splitFieldName := strings.Split(string(args.ID), ".")
	if len(splitFieldName) != 2 {
		return nil, errors.New("invalid ID passed to query")
	}
	typeName := splitFieldName[0]
	fieldName := splitFieldName[1]
	for _, def := range p.executableSchema.MergedSchema.Types {
		if def.Name != typeName {
			continue
		}
		var field *brambleField
		for _, f := range def.Fields {
			if f.Name != fieldName {
				continue
			}
			var svcName string
			if svcURL, err := p.executableSchema.Locations.URLFor(def.Name, "", f.Name); err == nil {
				svc := p.executableSchema.Services[svcURL]
				svcName = svc.Name
			}
			var args []brambleArg
			for _, a := range f.Arguments {
				args = append(args, brambleArg{
					Name: a.Name,
					Type: a.Type.String(),
				})
			}
			field = &brambleField{
				ID:          graphql.ID(def.Name + "." + f.Name),
				Name:        f.Name,
				Type:        f.Type.String(),
				Service:     svcName,
				Description: strToPtr(f.Description),
				Arguments:   args,
			}
			return field, nil
		}
	}
	return nil, nil
}

type brambleService struct {
	Name       string
	Version    string
	Schema     string
	Status     string
	ServiceURL string
}

func (s brambleService) Id() graphql.ID {
	return graphql.ID(s.Name)
}

type externalBrambleServices []brambleService

func (s externalBrambleServices) Len() int {
	return len(s)
}

func (s externalBrambleServices) Less(i, j int) bool {
	// unreachable services have no name
	if s[i].Name == s[j].Name {
		return s[i].ServiceURL < s[j].ServiceURL
	}
	return s[i].Name < s[j].Name
}

func (s externalBrambleServices) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (r *metaPluginResolver) Services() []brambleService {
	var services externalBrambleServices
	for _, element := range r.executableSchema.Services {
		services = append(services, brambleService{
			Name:       element.Name,
			Version:    element.Version,
			Schema:     element.SchemaSource,
			Status:     element.Status,
			ServiceURL: element.ServiceURL,
		})
	}
	sort.Sort(services)
	return services
}

type MetaPlugin struct {
	*bramble.BasePlugin
	resolver *metaPluginResolver
}

func NewMetaPlugin() *MetaPlugin {
	return &MetaPlugin{
		resolver: newMetaPluginResolver(),
	}
}

func (p *MetaPlugin) Init(s *bramble.ExecutableSchema) {
	p.resolver.executableSchema = s
}

func (i *MetaPlugin) ID() string {
	return "meta"
}

func (i *MetaPlugin) GraphqlQueryPath() (bool, string) {
	return true, "bramble-meta-plugin-query"
}

func (i *MetaPlugin) SetupPrivateMux(mux *http.ServeMux) {
	_, path := i.GraphqlQueryPath()
	s := graphql.MustParseSchema(metaPluginSchema, i.resolver, graphql.UseFieldResolvers())
	mux.Handle(fmt.Sprintf("/%s", path), &relay.Handler{Schema: s})
}
