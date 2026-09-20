package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// durationType is checked before any integer kind. A Duration is an int64
// underneath, so a switch on kind alone would fail to parse 33ms and would
// read a bare 33 as 33 nanoseconds - a setting that is silently a hundred
// million times too small.
var durationType = reflect.TypeFor[time.Duration]()

// set returns entry with one field written from assignment. The entry itself
// is never written through: a struct is copied, and a pointer is copied and
// re-pointed, so whatever the composition root still holds keeps the values it
// was built with.
func set(entry any, assignment Assignment) (any, error) {
	value := reflect.ValueOf(entry)
	pointer := value.Kind() == reflect.Pointer
	if pointer {
		if value.IsNil() {
			return nil, fmt.Errorf("%s: %s's configuration is a nil pointer, not a struct",
				assignment.Origin, assignment.Plugin)
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s: %s's configuration is %s, not a struct",
			assignment.Origin, assignment.Plugin, describe(value))
	}

	copied := reflect.New(value.Type())
	copied.Elem().Set(value)
	field, err := fieldOf(copied.Elem(), assignment)
	if err != nil {
		return nil, err
	}
	if err := parseInto(field, assignment); err != nil {
		return nil, err
	}
	if pointer {
		return copied.Interface(), nil
	}
	return copied.Elem().Interface(), nil
}

// fieldOf finds the field an assignment names. An exact spelling wins; failing
// that the name is matched case-insensitively with its underscores ignored,
// which is what lets COG_CANVAS_MAX_ATLAS_BYTES reach MaxAtlasBytes. Only the
// struct's own exported fields are candidates, so nothing promoted through an
// embedded struct is reachable and neither is unexported state.
func fieldOf(value reflect.Value, assignment Assignment) (reflect.Value, error) {
	structure := value.Type()
	if field, ok := structure.FieldByName(assignment.Field); ok && field.IsExported() && len(field.Index) == 1 {
		return value.Field(field.Index[0]), nil
	}

	wanted, found, name := canonical(assignment.Field), reflect.Value{}, ""
	for i := range structure.NumField() {
		field := structure.Field(i)
		if !field.IsExported() || canonical(field.Name) != wanted {
			continue
		}
		if name != "" {
			return reflect.Value{}, fmt.Errorf("%s: %s's %s and %s are both spelled %s",
				assignment.Origin, assignment.Plugin, name, field.Name, assignment.Field)
		}
		found, name = value.Field(i), field.Name
	}
	if name == "" {
		return reflect.Value{}, fmt.Errorf("%s: %s's configuration (%s) has no field %s",
			assignment.Origin, assignment.Plugin, structure, assignment.Field)
	}
	return found, nil
}

// parseInto writes an assignment's text into one field.
func parseInto(field reflect.Value, assignment Assignment) error {
	if field.Type() == durationType {
		if assignment.Bare {
			return bare(field, assignment)
		}
		duration, err := time.ParseDuration(assignment.Value)
		if err != nil {
			return unparsable(assignment, "a duration such as 33ms or 1s")
		}
		field.SetInt(int64(duration))
		return nil
	}

	switch kind := field.Kind(); kind {
	case reflect.Bool:
		if assignment.Bare {
			field.SetBool(true)
			return nil
		}
		parsed, err := strconv.ParseBool(assignment.Value)
		if err != nil {
			return unparsable(assignment, "a bool")
		}
		field.SetBool(parsed)
	case reflect.String:
		if assignment.Bare {
			return bare(field, assignment)
		}
		field.SetString(assignment.Value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if assignment.Bare {
			return bare(field, assignment)
		}
		parsed, err := strconv.ParseInt(assignment.Value, 10, field.Type().Bits())
		if err != nil {
			return unparsable(assignment, fmt.Sprintf("an %s", kind))
		}
		field.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if assignment.Bare {
			return bare(field, assignment)
		}
		parsed, err := strconv.ParseUint(assignment.Value, 10, field.Type().Bits())
		if err != nil {
			return unparsable(assignment, fmt.Sprintf("a %s", kind))
		}
		field.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		if assignment.Bare {
			return bare(field, assignment)
		}
		parsed, err := strconv.ParseFloat(assignment.Value, field.Type().Bits())
		if err != nil {
			return unparsable(assignment, fmt.Sprintf("a %s", kind))
		}
		field.SetFloat(parsed)
	default:
		return fmt.Errorf("%s: %s's %s is a %s, which no text can set",
			assignment.Origin, assignment.Plugin, assignment.Field, field.Type())
	}
	return nil
}

// bare reports a key given with no value against a field that is not a bool.
// Only a toggle can mean something without one.
func bare(field reflect.Value, assignment Assignment) error {
	return fmt.Errorf("%s: %s's %s is a %s and needs a value; only a bool may be named alone",
		assignment.Origin, assignment.Plugin, assignment.Field, field.Type())
}

// unparsable reports a value the field's type cannot read.
func unparsable(assignment Assignment, wanted string) error {
	return fmt.Errorf("%s: %s's %s takes %s, and %q is not one",
		assignment.Origin, assignment.Plugin, assignment.Field, wanted, assignment.Value)
}

// describe names what an entry is when it is not a struct, including the nil
// an untyped map value holds.
func describe(value reflect.Value) string {
	if !value.IsValid() {
		return "nil"
	}
	return value.Type().String()
}

// canonical folds a field name to what an environment variable can spell:
// upper case, underscores dropped.
func canonical(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "_", ""))
}
