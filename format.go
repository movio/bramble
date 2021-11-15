package bramble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
)

func indentPrefix(sb *strings.Builder, level int, suffix ...string) (int, error) {
	var err error
	total, count := 0, 0
	for i := 0; i <= level; i++ {
		count, err = sb.WriteString("    ")
		total += count
		if err != nil {
			return total, err
		}
	}
	for _, str := range suffix {
		count, err = sb.WriteString(str)
		total += count
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func formatSelectionSelectionSet(sb *strings.Builder, schema *ast.Schema, vars map[string]interface{}, level int, selectionSet ast.SelectionSet) {
	sb.WriteString(" {")
	formatSelection(sb, schema, vars, level+1, selectionSet)
	indentPrefix(sb, level, "}")
}

func formatSelection(sb *strings.Builder, schema *ast.Schema, vars map[string]interface{}, level int, selectionSet ast.SelectionSet) {
	for _, selection := range selectionSet {
		indentPrefix(sb, level)
		switch selection := selection.(type) {
		case *ast.Field:
			if selection.Alias != selection.Name {
				sb.WriteString(selection.Alias)
				sb.WriteString(": ")
				sb.WriteString(selection.Name)
			} else {
				sb.WriteString(selection.Alias)
			}
			formatArgumentList(sb, schema, vars, selection.Arguments)
			for _, d := range selection.Directives {
				sb.WriteString(" @")
				sb.WriteString(d.Name)
				formatArgumentList(sb, schema, vars, d.Arguments)
			}
			if len(selection.SelectionSet) > 0 {
				formatSelectionSelectionSet(sb, schema, vars, level, selection.SelectionSet)
			}
		case *ast.InlineFragment:
			fmt.Fprintf(sb, "... on %v", selection.TypeCondition)
			formatSelectionSelectionSet(sb, schema, vars, level, selection.SelectionSet)
		case *ast.FragmentSpread:
			sb.WriteString("...")
			sb.WriteString(selection.Name)
		}
	}
}

func formatArgumentList(sb *strings.Builder, schema *ast.Schema, vars map[string]interface{}, args ast.ArgumentList) {
	if len(args) > 0 {
		sb.WriteString("(")
		for i, arg := range args {
			if i != 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(sb, "%s: %s", arg.Name, formatArgument(schema, arg.Value, vars))
		}
		sb.WriteString(")")
	}
}

func formatSelectionSet(ctx context.Context, schema *ast.Schema, selection ast.SelectionSet) string {
	vars := map[string]interface{}{}
	if reqctx := graphql.GetOperationContext(ctx); reqctx != nil {
		vars = reqctx.Variables
	}

	sb := strings.Builder{}

	sb.WriteString("{")
	formatSelection(&sb, schema, vars, 0, selection)
	sb.WriteString("\n}")

	return sb.String()
}

var multipleSpacesRegex = regexp.MustCompile(`\s+`)

func formatSelectionSetSingleLine(ctx context.Context, schema *ast.Schema, selection ast.SelectionSet) string {
	return multipleSpacesRegex.ReplaceAllString(formatSelectionSet(ctx, schema, selection), " ")
}

func formatArgument(schema *ast.Schema, v *ast.Value, vars map[string]interface{}) string {
	if schema == nil {
		// this is to allow tests to pass to due the MarshalJSON comparator not having access
		// to the schema
		return v.String()
	}

	// this is a mix between v.String() and v.Raw(vars) as we need a string value with variables replaced

	if v == nil {
		return "<nil>"
	}
	switch v.Kind {
	case ast.Variable:
		return expandAndFormatVariable(schema, schema.Types[v.ExpectedType.Name()], vars[v.Raw])
	case ast.IntValue, ast.FloatValue, ast.EnumValue, ast.BooleanValue, ast.NullValue:
		return v.Raw
	case ast.StringValue, ast.BlockValue:
		return strconv.Quote(v.Raw)
	case ast.ListValue:
		var val []string
		for _, elem := range v.Children {
			val = append(val, formatArgument(schema, elem.Value, vars))
		}
		return "[" + strings.Join(val, ",") + "]"
	case ast.ObjectValue:
		var val []string
		for _, elem := range v.Children {
			val = append(val, elem.Name+":"+formatArgument(schema, elem.Value, vars))
		}
		return "{" + strings.Join(val, ",") + "}"
	default:
		panic(fmt.Errorf("unknown value kind %d", v.Kind))
	}
}

func expandAndFormatVariable(schema *ast.Schema, objectType *ast.Definition, v interface{}) string {
	if v == nil {
		return "null"
	}

	switch objectType.Kind {
	case ast.Scalar:
		b, _ := json.Marshal(v)
		return string(b)
	case ast.Enum:
		return fmt.Sprint(v)
	case ast.Object, ast.InputObject, ast.Interface, ast.Union:
		switch v := v.(type) {
		case map[string]interface{}:
			var buf strings.Builder
			buf.WriteString("{")

			for i, f := range objectType.Fields {
				if i != 0 {
					buf.WriteString(" ")
				}

				fieldName := f.Name
				value, ok := v[fieldName]
				if !ok {
					continue
				}

				// if it's a list we call recursively on every element
				if f.Type.Elem != nil {
					switch reflect.TypeOf(value).Kind() {
					case reflect.Slice:
						s := reflect.ValueOf(value)
						var elems []string
						for i := 0; i < s.Len(); i++ {
							elems = append(elems, expandAndFormatVariable(schema, schema.Types[f.Type.Elem.Name()], s.Index(i).Interface()))
						}
						fmt.Fprintf(&buf, "%s: [%s]", fieldName, strings.Join(elems, ", "))
						continue
					default:
						panic("invalid type, expected slice")
					}
				}

				fmt.Fprintf(&buf, "%s: %s", fieldName, expandAndFormatVariable(schema, schema.Types[f.Type.Name()], value))
			}

			buf.WriteString("}")
			return buf.String()
		case []interface{}:
			var val []string
			for _, elem := range v {
				val = append(val, expandAndFormatVariable(schema, objectType, elem))
			}
			return "[" + strings.Join(val, ",") + "]"
		default:
			panic("unknown type " + reflect.TypeOf(v).String())
		}
	}

	return ""
}

func formatSchema(schema *ast.Schema) string {
	buf := bytes.NewBufferString("")
	f := formatter.NewFormatter(buf)
	f.FormatSchema(schema)
	return buf.String()
}
