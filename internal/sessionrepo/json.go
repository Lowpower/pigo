package sessionrepo

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

func invalidPayload(reason string) error {
	return NewError(ErrInvalidPayload, "Durable payload "+reason)
}

// AssertJSONSerializable rejects values that JSON cannot round-trip faithfully.
func AssertJSONSerializable(value any) error {
	seen := map[uintptr]struct{}{}
	return walkJSON(reflect.ValueOf(value), seen)
}

func walkJSON(v reflect.Value, seen map[uintptr]struct{}) error {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		if v.Kind() == reflect.Pointer {
			ptr := v.Pointer()
			if _, ok := seen[ptr]; ok {
				return invalidPayload("contains a cycle")
			}
			seen[ptr] = struct{}{}
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Invalid:
		return nil
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return invalidPayload("contains a non-finite number")
		}
		return nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice {
			if v.IsNil() || v.Len() == 0 {
				return nil
			}
			if ptr := v.Pointer(); ptr != 0 {
				if _, ok := seen[ptr]; ok {
					return invalidPayload("contains a cycle")
				}
				seen[ptr] = struct{}{}
			}
		}
		for i := 0; i < v.Len(); i++ {
			if err := walkJSON(v.Index(i), seen); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return invalidPayload("contains a non-plain object")
		}
		if v.Len() > 0 {
			if ptr := v.Pointer(); ptr != 0 {
				if _, ok := seen[ptr]; ok {
					return invalidPayload("contains a cycle")
				}
				seen[ptr] = struct{}{}
			}
		}
		for _, key := range v.MapKeys() {
			if err := walkJSON(v.MapIndex(key), seen); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if !t.Field(i).IsExported() {
				continue
			}
			if err := walkJSON(v.Field(i), seen); err != nil {
				return err
			}
		}
		return nil
	default:
		return invalidPayload(fmt.Sprintf("contains %s", v.Kind()))
	}
}

func cloneJSON[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func cloneEntry(e Entry) Entry {
	out := cloneJSON(e)
	out.HasData = e.HasData
	out.HasDetails = e.HasDetails
	out.HasTokensBefore = e.HasTokensBefore
	if e.Terminate != nil {
		t := *e.Terminate
		out.Terminate = &t
	}
	return out
}

func cloneRecord(r Record) Record {
	out := cloneJSON(r)
	out.HasRunID = r.HasRunID || r.RunID != ""
	return out
}

func cloneMeta(m Metadata) Metadata {
	out := cloneJSON(m)
	out.HasName = m.HasName
	if m.Metadata != nil {
		out.Metadata = cloneJSON(m.Metadata)
	}
	return out
}
