// Package attrs provides typed access to attestation attributes.
//
// Attestation attributes are stored as map[string]interface{} (JSON).
// This package bridges between the schemaless bag and typed Go structs
// using struct tags.
//
// Usage:
//
//	type PromptAttrs struct {
//	    Template string `attr:"template"`
//	    Version  int    `attr:"version"`
//	    Model    string `attr:"model,omitempty"`
//	}
//
//	// Read: map → struct
//	var p PromptAttrs
//	attrs.Scan(as.Attributes, &p)
//
//	// Write: struct → map
//	as.Attributes = attrs.From(p)
package attrs

import (
	"reflect"
	"strings"
)

// Scan reads values from a map[string]interface{} into a struct using `attr` tags.
// Fields without a matching key are left at their zero value.
// Handles JSON number coercion (float64 → int).
func Scan(m map[string]any, dst any) {
	if m == nil {
		return
	}

	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := tagKey(field)
		if key == "" {
			continue
		}

		val, ok := m[key]
		if !ok || val == nil {
			continue
		}

		setField(v.Field(i), val)
	}
}

// From converts a struct into map[string]interface{} using `attr` tags.
// Fields tagged with "omitempty" are skipped when at their zero value.
func From(src any) map[string]any {
	v := reflect.ValueOf(src)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	t := v.Type()
	m := make(map[string]any)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("attr")
		if tag == "" || tag == "-" {
			continue
		}

		key, omitempty := parseTag(tag)
		fv := v.Field(i)

		if omitempty && fv.IsZero() {
			continue
		}

		// Dereference pointers for clean map values
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}

		m[key] = fv.Interface()
	}

	return m
}

func tagKey(f reflect.StructField) string {
	tag := f.Tag.Get("attr")
	if tag == "" || tag == "-" {
		return ""
	}
	key, _ := parseTag(tag)
	return key
}

func parseTag(tag string) (key string, omitempty bool) {
	parts := strings.SplitN(tag, ",", 2)
	key = parts[0]
	if len(parts) > 1 && parts[1] == "omitempty" {
		omitempty = true
	}
	return
}

func setField(fv reflect.Value, val any) {
	switch fv.Kind() {
	case reflect.String:
		if s, ok := val.(string); ok {
			fv.SetString(s)
		}

	case reflect.Int, reflect.Int64:
		switch n := val.(type) {
		case float64:
			fv.SetInt(int64(n))
		case int:
			fv.SetInt(int64(n))
		case int64:
			fv.SetInt(n)
		}

	case reflect.Float64:
		if n, ok := val.(float64); ok {
			fv.SetFloat(n)
		}

	case reflect.Bool:
		if b, ok := val.(bool); ok {
			fv.SetBool(b)
		}

	case reflect.Slice:
		if fv.Type().Elem().Kind() == reflect.String {
			switch items := val.(type) {
			case []string:
				fv.Set(reflect.ValueOf(items))
			case []any:
				strs := make([]string, 0, len(items))
				for _, item := range items {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				fv.Set(reflect.ValueOf(strs))
			}
		}

	case reflect.Pointer:
		// *float64, *bool, etc.
		if fv.Type().Elem().Kind() == reflect.Float64 {
			if n, ok := val.(float64); ok {
				fv.Set(reflect.ValueOf(&n))
			}
		}
		if fv.Type().Elem().Kind() == reflect.Bool {
			if b, ok := val.(bool); ok {
				fv.Set(reflect.ValueOf(&b))
			}
		}
	}
}
