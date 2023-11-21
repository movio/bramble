package plugins

import (
	"context"
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
	fields: [BrambleField!]!
	types: [BrambleType!]!
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
	type(id: ID!): BrambleType
	service(id: ID!): BrambleService
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
	metaResolver     *metaResolver
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
		metaResolver: &metaResolver{},
	}
}

func (r *metaPluginResolver) Meta() *metaResolver {
	return r.metaResolver
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

func (r *metaResolver) Schema() (*brambleSchema, error) {
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
	return r.metaResolver.GetService(ctx, args)
}

func (r *metaResolver) GetService(ctx context.Context, args struct{ ID graphql.ID }) *brambleService {
	for _, service := range r.Services() {
		if service.Name == string(args.ID) {
			return &service
		}
	}

	return nil
}

func (r *metaPluginResolver) GetType(ctx context.Context, args struct{ ID graphql.ID }) (*brambleType, error) {
	return r.metaResolver.GetType(ctx, args)
}

func (r *metaResolver) GetType(ctx context.Context, args struct{ ID graphql.ID }) (*brambleType, error) {
	typeName := string(args.ID)
	for _, t := range r.getTypes(r.executableSchema.MergedSchema) {
		if t.Name == typeName {
			return &t, nil
		}
	}

	return nil, nil
}

func (r *metaResolver) getTypes(schema *ast.Schema) []brambleType {
	if schema == nil {
		return nil
	}
	var result []brambleType
	for _, def := range schema.Types {
		result = append(result, r.brambleType(def.Name, def))
	}

	return result
}

func (r *metaResolver) brambleType(name string, def *ast.Definition) brambleType {
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

func (r *metaResolver) Field(ctx context.Context, args struct{ ID graphql.ID }) (*brambleField, error) {
	return r.GetField(ctx, args)
}

func (r *metaResolver) Type(ctx context.Context, args struct{ ID graphql.ID }) (*brambleType, error) {
	return r.GetType(ctx, args)
}

func (r *metaResolver) Service(ctx context.Context, args struct{ ID graphql.ID }) (*brambleService, error) {
	return r.GetService(ctx, args), nil
}

type metaResolver struct {
	executableSchema *bramble.ExecutableSchema
}

func (r *metaPluginResolver) GetField(ctx context.Context, args struct{ ID graphql.ID }) (*brambleField, error) {
	return r.metaResolver.GetField(ctx, args)
}

func (r *metaResolver) GetField(ctx context.Context, args struct{ ID graphql.ID }) (*brambleField, error) {
	for _, f := range r.getFields(r.executableSchema.MergedSchema) {
		if f.ID == args.ID {
			return &f, nil
		}
	}

	return nil, nil
}

func (r *metaResolver) getFields(schema *ast.Schema) []brambleField {
	if schema == nil {
		return nil
	}
	var result []brambleField
	for _, def := range schema.Types {
		for _, f := range def.Fields {
			var svcName string
			if svcURL, err := r.executableSchema.Locations.URLFor(def.Name, "", f.Name); err == nil {
				if svc := r.executableSchema.Services[svcURL]; svc != nil {
					svcName = svc.Name
				}
			}
			var args []brambleArg
			for _, a := range f.Arguments {
				args = append(args, brambleArg{
					Name: a.Name,
					Type: a.Type.String(),
				})
			}

			result = append(result, brambleField{
				ID:          graphql.ID(def.Name + "." + f.Name),
				Name:        f.Name,
				Type:        f.Type.String(),
				Service:     svcName,
				Description: strToPtr(f.Description),
				Arguments:   args,
			})
		}
	}

	return result
}

type brambleService struct {
	Name       string
	Version    string
	Schema     string
	Status     string
	ServiceURL string
	Fields     []brambleField
	Types      []brambleType
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

func (r *metaResolver) Services() []brambleService {
	var services externalBrambleServices
	for _, element := range r.executableSchema.Services {
		services = append(services, brambleService{
			Name:       element.Name,
			Version:    element.Version,
			Schema:     element.SchemaSource,
			Status:     element.Status,
			ServiceURL: element.ServiceURL,
			Fields:     r.getFields(element.Schema),
			Types:      r.getTypes(element.Schema),
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
	p.resolver.metaResolver.executableSchema = s
}

func (p *MetaPlugin) ID() string {
	return "meta"
}

func (p *MetaPlugin) GraphqlQueryPath() (bool, string) {
	return true, "bramble-meta-plugin-query"
}

func (p *MetaPlugin) SetupPrivateMux(mux *http.ServeMux) {
	_, path := p.GraphqlQueryPath()
	s := graphql.MustParseSchema(metaPluginSchema, p.resolver, graphql.UseFieldResolvers())
	mux.Handle(fmt.Sprintf("/%s", path), &relay.Handler{Schema: s})
}
