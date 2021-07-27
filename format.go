package bramble

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// marshalResult marshals the result map according to the field order specified
// in the selection set and the (non)-nullability of fields.
// If a non-nullable field is null, the null value will bubble up to the next
// nullable field.
func marshalResult(data interface{}, selectionSet ast.SelectionSet, schema *ast.Schema, currentType *ast.Type) ([]byte, error) {
	var buf bytes.Buffer
	var err error

	if currentType == nil {
		return []byte("null"), fmt.Errorf("currentType is nil, unable to marshal data")
	}

	if schema.Types[currentType.Name()].Kind == ast.Scalar {
		if len(selectionSet) != 0 {
			return []byte("null"), errors.New("non-empty selection set on scalar type")
		}

		b, err := json.Marshal(data)
		if err != nil {
			return []byte("null"), err
		}
		return b, nil
	}

	switch data := data.(type) {
	case json.RawMessage:
		return data, nil
	case map[string]interface{}:
		if data == nil {
			return []byte("null"), nil
		}

		def := schema.Types[getInnerTypeName(currentType)]
		if def == nil {
			return []byte("null"), fmt.Errorf("could not find type %q in schema", currentType.String())
		}

		buf.WriteString("{")
		fields := selectionSetToFieldsWithTypeCondition(selectionSet)
		for i, fieldWithOptionalTypeCondition := range fields {
			field := fieldWithOptionalTypeCondition.field
			if fieldWithOptionalTypeCondition.typeCondition != "" {
				def = schema.Types[fieldWithOptionalTypeCondition.typeCondition]
				if def == nil {
					return []byte("null"), fmt.Errorf("could not find type %q in schema", currentType.String())
				}
			}
			var fieldType *ast.Type
			if field.Name == "__typename" {
				fieldType = ast.NamedType("String", nil)
			} else if fieldDef := def.Fields.ForName(field.Name); fieldDef != nil {
				fieldType = fieldDef.Type
			}
			if fieldType == nil {
				return []byte("null"), fmt.Errorf("could not find field %q in %q", field.Name, currentType.String())
			}

			key, fieldErr := json.Marshal(field.Alias)
			if fieldErr != nil {
				return nil, fieldErr
			}
			buf.Write(key)
			buf.WriteString(`:`)
			d, ok := data[field.Alias]
			var value []byte
			if !ok {
				value = []byte("null")
			} else {
				value, fieldErr = marshalResult(d, field.SelectionSet, schema, fieldType)
			}
			if fieldType.NonNull && bytes.Equal(value, []byte("null")) {
				if fieldErr == nil {
					fieldErr = fmt.Errorf("got a null response for non-nullable field %q", field.Alias)
				}
				return []byte("null"), fieldErr
			}
			buf.Write(value)
			if i != len(fields)-1 {
				buf.WriteString(",")
			}

			if fieldErr != nil {
				err = fieldErr
			}
		}
		buf.WriteString("}")
	case []map[string]interface{}:
		if data == nil {
			return []byte("null"), nil
		}

		elemType := currentType.Elem
		if elemType == nil {
			return []byte("null"), fmt.Errorf("type %q should be a list but element is nil", currentType.String())
		}

		buf.WriteString("[")
		for i, e := range data {
			b, eltErr := marshalResult(e, selectionSet, schema, currentType.Elem)
			if eltErr != nil {
				err = eltErr
			}
			if elemType.NonNull && bytes.Equal(b, []byte("null")) {
				if eltErr == nil {
					eltErr = fmt.Errorf("got null element in list of non-null elements")
				}
				return []byte("null"), eltErr
			}
			buf.Write(b)
			if i != len(data)-1 {
				buf.WriteString(",")
			}
		}
		buf.WriteString("]")
	case []interface{}:
		if data == nil {
			return []byte("null"), nil
		}

		elemType := currentType.Elem
		if elemType == nil {
			return []byte("null"), fmt.Errorf("type %q should be a list but element is nil", currentType.String())
		}

		buf.WriteString("[")
		for i, value := range data {
			valueBytes, valueErr := marshalResult(value, selectionSet, schema, currentType.Elem)
			if valueErr != nil {
				err = valueErr
			}
			if elemType.NonNull && bytes.Equal(valueBytes, []byte("null")) {
				if valueErr == nil {
					valueErr = fmt.Errorf("got null element in list of non-null elements")
				}
				return []byte("null"), valueErr
			}
			buf.Write(valueBytes)
			if i != len(data)-1 {
				buf.WriteString(",")
			}
		}
		buf.WriteString("]")
	default:
		b, err := json.Marshal(data)
		if err != nil {
			return []byte("null"), err
		}

		return b, nil
	}

	return buf.Bytes(), err
}

type fieldWithOptionalTypeCondition struct {
	field         *ast.Field
	typeCondition string
}

// When walking through a fragment spread we need to preserve the TypeCondition as it contains the target
// type of the spread.
func selectionSetToFieldsWithTypeCondition(selectionSet ast.SelectionSet) []fieldWithOptionalTypeCondition {
	var result []fieldWithOptionalTypeCondition
	for _, selection := range selectionSet {
		switch selection := selection.(type) {
		case *ast.Field:
			result = append(result, fieldWithOptionalTypeCondition{field: selection, typeCondition: ""})
		case *ast.FragmentSpread:
			fragmentSpreadFields := selectionSetToFields(selection.Definition.SelectionSet)
			for _, field := range fragmentSpreadFields {
				result = append(result, fieldWithOptionalTypeCondition{
					field:         field,
					typeCondition: selection.Definition.TypeCondition,
				})
			}
		case *ast.InlineFragment:
			inlineFragmentFields := selectionSetToFields(selection.SelectionSet)
			for _, field := range inlineFragmentFields {
				result = append(result, fieldWithOptionalTypeCondition{
					field:         field,
					typeCondition: selection.TypeCondition,
				})
			}
		}
	}
	return result
}

func getInnerTypeName(t *ast.Type) string {
	if t.Elem != nil {
		return getInnerTypeName(t.Elem)
	}

	return t.Name()
}

func formatSchema(schema *ast.Schema) string {
	buf := bytes.NewBufferString("")
	f := formatter.NewFormatter(buf)
	f.FormatSchema(schema)
	return buf.String()
}
