package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// SchemaFromStruct derives a minimal JSON schema from a struct type.
func SchemaFromStruct(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, fmt.Errorf("nil value")
	}
	typeOf := reflect.TypeOf(value)
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", typeOf.Kind())
	}

	schema := buildSchemaFromStruct(typeOf)
	payload, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

type jsonSchema struct {
	Type        string                `json:"type,omitempty"`
	Properties  map[string]jsonSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Items       *jsonSchema           `json:"items,omitempty"`
	Format      string                `json:"format,omitempty"`
	Description string                `json:"description,omitempty"`
}

func buildSchemaFromStruct(t reflect.Type) jsonSchema {
	properties := make(map[string]jsonSchema)
	required := make([]string, 0)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		name, omitEmpty := jsonFieldName(field)
		if name == "" {
			continue
		}
		fieldSchema := schemaForType(field.Type)
		desc := field.Tag.Get("desc")
		if desc != "" {
			fieldSchema.Description = desc
		}
		properties[name] = fieldSchema
		if !omitEmpty {
			required = append(required, name)
		}
	}

	return jsonSchema{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	if tag == "" {
		return field.Name, false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = field.Name
	}
	omitEmpty := false
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitEmpty = true
			break
		}
	}
	return name, omitEmpty
}

func schemaForType(t reflect.Type) jsonSchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return jsonSchema{Type: "string"}
	case reflect.Bool:
		return jsonSchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return jsonSchema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return jsonSchema{Type: "number"}
	case reflect.Slice, reflect.Array:
		itemSchema := schemaForType(t.Elem())
		return jsonSchema{Type: "array", Items: &itemSchema}
	case reflect.Struct:
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return jsonSchema{Type: "string", Format: "date-time"}
		}
		nested := buildSchemaFromStruct(t)
		return nested
	default:
		return jsonSchema{Type: "string"}
	}
}
