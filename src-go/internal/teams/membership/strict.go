package membership

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// decodeStrict reads one JSON object into v the way pingbus's Python reads the same files:
// a key matches its field's name exactly (encoding/json also matches it in any case), a key
// given twice is refused, and so is a key the struct does not name or text after the object.
func decodeStrict(data []byte, v any) error {
	doc, err := pyjson.DecodeUTF8(data, pyjson.Options{NoDuplicateKeys: true})
	if err != nil {
		return err
	}
	if err := exactKeys(doc, reflect.TypeOf(v).Elem()); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// exactKeys is nil when every object in doc names only fields of t, spelt exactly. A type that
// is not a struct (a raw message, a scalar) is not looked into.
func exactKeys(doc any, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch v := doc.(type) {
	case pyjson.Object:
		if t.Kind() == reflect.Struct {
			return exactObjectKeys(v, t)
		}
	case []any:
		if t.Kind() == reflect.Slice {
			return exactListKeys(v, t.Elem())
		}
	}
	return nil
}

func exactObjectKeys(obj pyjson.Object, t reflect.Type) error {
	fields := jsonFields(t)
	for key, val := range obj {
		field, ok := fields[key]
		if !ok {
			return fmt.Errorf("the key %q is not spelt as any field is", key)
		}
		if err := exactKeys(val, field); err != nil {
			return err
		}
	}
	return nil
}

func exactListKeys(list []any, elem reflect.Type) error {
	for _, item := range list {
		if err := exactKeys(item, elem); err != nil {
			return err
		}
	}
	return nil
}

// jsonFields maps each json name of struct type t to its field's type.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, t.NumField())
	for f := range t.Fields() {
		name := strings.TrimSuffix(f.Tag.Get("json"), ",omitempty")
		if name != "" && name != "-" {
			fields[name] = f.Type
		}
	}
	return fields
}
