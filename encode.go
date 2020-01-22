package avro

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/rogpeppe/gogen-avro/v7/schema"
)

// Set to true for deterministic output.
const sortMapKeys = false

type encoderInfo struct {
	encode   encoderFunc
	avroType *Type
}

// Marshal encodes x as a message using the Avro binary
// encoding, using TypeOf(x) as the Avro type for marshaling.
//
// Marshal returns the encoded data and the actual type that
// was used for marshaling.
//
// See https://avro.apache.org/docs/current/spec.html#binary_encoding
func Marshal(x interface{}) ([]byte, *Type, error) {
	return marshalAppend(globalNames, nil, reflect.ValueOf(x))
}

// Marshal is like the Marshal function except that names
// in the schema for x are renamed according to names.
func (names *Names) Marshal(x interface{}) ([]byte, *Type, error) {
	return marshalAppend(names, nil, reflect.ValueOf(x))
}

func marshalAppend(names *Names, buf []byte, xv reflect.Value) (_ []byte, _ *Type, marshalErr error) {
	avroType, enc := typeEncoder0(names, nil, xv.Type(), azTypeInfo{})
	if avroType == nil {
		avroType1, err := avroTypeOf(names, xv.Type())
		if err != nil {
			// Shouldn't be able to happen.
			return nil, nil, err
		}
		avroType = avroType1
		names.goTypeToEncoder.Store(xv.Type(), &encoderInfo{
			avroType: avroType,
			encode:   enc,
		})
	}
	e := &encodeState{
		Buffer: bytes.NewBuffer(buf),
	}
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(*encodeError); ok {
				marshalErr = err.err
			} else {
				panic(r)
			}
		}
	}()
	enc(e, xv)
	return e.Bytes(), avroType, nil
}

type encodeState struct {
	*bytes.Buffer
	scratch [64]byte
}

// error aborts the encoding by panicking with err wrapped in encodeError.
func (e *encodeState) error(err error) {
	panic(&encodeError{err})
}

func errorEncoder(err error) encoderFunc {
	return func(e *encodeState, v reflect.Value) {
		e.error(err)
	}
}

type encodeError struct {
	err error
}

type encoderFunc func(e *encodeState, v reflect.Value)

func typeEncoder(names *Names, at schema.AvroType, t reflect.Type, info azTypeInfo) encoderFunc {
	_, enc := typeEncoder0(names, at, t, info)
	return enc
}

func typeEncoder0(names *Names, at schema.AvroType, t reflect.Type, info azTypeInfo) (*Type, encoderFunc) {
	// Note: since a Go type can't encode as more than one definition,
	// we can use a purely Go-type-based cache.
	enc0, ok := names.goTypeToEncoder.Load(t)
	if ok {
		info := enc0.(*encoderInfo)
		return info.avroType, info.encode
	}
	var at1 *Type
	if at == nil {
		// We haven't been given an Avro type, which happens
		// when definitionEncoder is called at the top level.
		// Allowing this means we can do just a single cache
		// lookup rather than two (one for type->avro type, one
		// for encoder). The need for it wouldn't be there if
		// Marshal didn't return the Avro type, but that's quite
		// nice, so here we are.
		var err error
		at1, err = avroTypeOf(names, t)
		if err != nil {
			return nil, errorEncoder(err)
		}
		at = at1.avroType
	}
	enc := typeEncoderUncached(names, at, t, info)
	// Note that for non-top-level calls, at1 will
	// be nil - it can be calculated and cached later
	// if this type is ever used directly.
	names.goTypeToEncoder.LoadOrStore(t, &encoderInfo{
		avroType: at1,
		encode:   enc,
	})
	return at1, enc
}

// typeEncoder returns an encoder that encodes values of type t according
// to the Avro type at,
func typeEncoderUncached(names *Names, at schema.AvroType, t reflect.Type, info azTypeInfo) encoderFunc {
	// TODO cache this so it's faster and so that we can deal with recursive types.
	switch at := at.(type) {
	case *schema.Reference:
		switch def := at.Def.(type) {
		case *schema.RecordDefinition:
			if t.Kind() != reflect.Struct {
				return errorEncoder(fmt.Errorf("expected struct"))
			}
			if len(info.entries) == 0 {
				// The type itself might contribute information.
				info1, err := newAzTypeInfo(t)
				if err != nil {
					return errorEncoder(fmt.Errorf("cannot get info for %s: %v", info.ftype, err))
				}
				info = info1
			}
			if len(info.entries) != len(def.Fields()) {
				return errorEncoder(fmt.Errorf("entry count mismatch (info entries %d vs definition fields %d; %s vs %s)", len(info.entries), len(def.Fields()), t, def.Name()))
			}
			fieldEncoders := make([]encoderFunc, len(def.Fields()))
			indexes := make([]int, len(def.Fields()))
			for i, f := range def.Fields() {
				fieldIndex := info.entries[i].fieldIndex
				fieldEncoders[i] = typeEncoder(names, f.Type(), t.Field(fieldIndex).Type, info.entries[i])
				indexes[i] = fieldIndex
			}
			return structEncoder{
				fieldEncoders: fieldEncoders,
				fieldIndexes:  indexes,
			}.encode
		case *schema.EnumDefinition:
			return longEncoder
		case *schema.FixedDefinition:
			return fixedEncoder{def.SizeBytes()}.encode
		default:
			return errorEncoder(fmt.Errorf("unknown definition type %T", def))
		}
	case *schema.UnionField:
		atypes := at.ItemTypes()
		switch t.Kind() {
		case reflect.Ptr:
			// It's a union of null and one other type, represented by a Go pointer.
			if len(atypes) != 2 {
				return errorEncoder(fmt.Errorf("unexpected item type count in union"))
			}
			switch {
			case info.entries[0].ftype == nil:
				return ptrUnionEncoder{
					indexes:    [2]byte{0, 1},
					encodeElem: typeEncoder(names, atypes[1], info.entries[1].ftype, info.entries[1]),
				}.encode
			case info.entries[1].ftype == nil:
				return ptrUnionEncoder{
					indexes:    [2]byte{1, 0},
					encodeElem: typeEncoder(names, atypes[0], info.entries[0].ftype, info.entries[0]),
				}.encode
			default:
				return errorEncoder(fmt.Errorf("unexpected types in union"))
			}
		case reflect.Interface:
			enc := unionEncoder{
				nullIndex: -1,
				choices:   make([]unionEncoderChoice, len(info.entries)),
			}
			for i, entry := range info.entries {
				if entry.ftype == nil {
					enc.nullIndex = i
				} else {
					enc.choices[i] = unionEncoderChoice{
						typ: entry.ftype,
						enc: typeEncoder(names, atypes[i], entry.ftype, entry),
					}
				}
			}
			return enc.encode
		default:
			return errorEncoder(fmt.Errorf("union type is not pointer or interface"))
		}
	case *schema.MapField:
		return mapEncoder{typeEncoder(names, at.ItemType(), t.Elem(), info)}.encode
	case *schema.ArrayField:
		return arrayEncoder{typeEncoder(names, at.ItemType(), t.Elem(), info)}.encode
	case *schema.BoolField:
		return boolEncoder
	case *schema.BytesField:
		return bytesEncoder
	case *schema.DoubleField:
		return doubleEncoder
	case *schema.FloatField:
		return floatEncoder
	case *schema.IntField:
		return longEncoder
	case *schema.LongField:
		if t == timeType {
			if lt := logicalType(at); lt == timestampMicros {
				return timestampMicrosEncoder
			} else {
				// TODO timestamp-millis support.
				return errorEncoder(fmt.Errorf("cannot encode time.Time as long with logical type %q", lt))
			}
		}
		return longEncoder
	case *schema.StringField:
		return stringEncoder
	default:
		return errorEncoder(fmt.Errorf("unknown avro schema type %T", at))
	}
}

func logicalType(t schema.AvroType) string {
	// Until https://github.com/actgardner/gogen-avro/issues/119
	// is fixed, we can't access metadata in general without a
	// race condition, so implement logicalType only
	// for the types that we currently care about, which
	// don't mutate themselves when Definition is called.
	switch t := t.(type) {
	case *schema.LongField, *schema.IntField:
		defn, _ := t.Definition(emptyScope())
		defn1, _ := defn.(map[string]interface{})
		lt, _ := defn1["logicalType"].(string)
		return lt
	}
	return ""
}

func timestampMillisEncoder(e *encodeState, v reflect.Value) {
	t := v.Interface().(time.Time)
	if t.IsZero() {
		e.writeLong(0)
	} else {
		e.writeLong(t.Unix()*1e3 + int64(t.Nanosecond())/int64(time.Millisecond))
	}
}

func timestampMicrosEncoder(e *encodeState, v reflect.Value) {
	t := v.Interface().(time.Time)
	if t.IsZero() {
		e.writeLong(0)
	} else {
		e.writeLong(t.Unix()*1e6 + int64(t.Nanosecond())/int64(time.Microsecond))
	}
}

type fixedEncoder struct {
	size int
}

func (fe fixedEncoder) encode(e *encodeState, v reflect.Value) {
	if v.CanAddr() {
		e.Write(v.Slice(0, fe.size).Bytes())
	} else {
		// TODO use a sync.Pool?
		buf := make([]byte, fe.size)
		reflect.Copy(reflect.ValueOf(buf), v)
		e.Write(buf)
	}
}

type mapEncoder struct {
	encodeElem encoderFunc
}

func (me mapEncoder) encode(e *encodeState, v reflect.Value) {
	n := v.Len()
	e.writeLong(int64(n))
	if n == 0 {
		return
	}
	if sortMapKeys {
		keys := make([]string, 0, n)
		for iter := v.MapRange(); iter.Next(); {
			keys = append(keys, iter.Key().String())
		}
		sort.Strings(keys)
		for _, k := range keys {
			kv := reflect.ValueOf(k)
			stringEncoder(e, kv)
			me.encodeElem(e, v.MapIndex(kv))
		}
	} else {
		for iter := v.MapRange(); iter.Next(); {
			stringEncoder(e, iter.Key())
			me.encodeElem(e, iter.Value())
		}
	}
	e.writeLong(0)
}

type arrayEncoder struct {
	encodeElem encoderFunc
}

func (ae arrayEncoder) encode(e *encodeState, v reflect.Value) {
	n := v.Len()
	e.writeLong(int64(n))
	if n == 0 {
		return
	}
	for i := 0; i < n; i++ {
		ae.encodeElem(e, v.Index(i))
	}
	e.writeLong(0)
}

func boolEncoder(e *encodeState, v reflect.Value) {
	if v.Bool() {
		e.WriteByte(1)
	} else {
		e.WriteByte(0)
	}
}

func longEncoder(e *encodeState, v reflect.Value) {
	e.writeLong(v.Int())
}

func (e *encodeState) writeLong(x int64) {
	n := binary.PutVarint(e.scratch[:], x)
	e.Write(e.scratch[:n])
}

func floatEncoder(e *encodeState, v reflect.Value) {
	binary.LittleEndian.PutUint32(e.scratch[:], math.Float32bits(float32(v.Float())))
	e.Write(e.scratch[:4])
}

func doubleEncoder(e *encodeState, v reflect.Value) {
	binary.LittleEndian.PutUint64(e.scratch[:], math.Float64bits(v.Float()))
	e.Write(e.scratch[:8])
}

func bytesEncoder(e *encodeState, v reflect.Value) {
	data := v.Bytes()
	e.writeLong(int64(len(data)))
	e.Write(data)
}

func stringEncoder(e *encodeState, v reflect.Value) {
	s := v.String()
	e.writeLong(int64(len(s)))
	e.WriteString(s)
}

type structEncoder struct {
	fieldIndexes  []int
	fieldEncoders []encoderFunc
}

func (se structEncoder) encode(e *encodeState, v reflect.Value) {
	for i, index := range se.fieldIndexes {
		se.fieldEncoders[i](e, v.Field(index))
	}
}

type unionEncoderChoice struct {
	typ reflect.Type
	enc encoderFunc
}

type unionEncoder struct {
	// nullIndex holds the union index of the null alternative,
	// or -1 if there is none.
	nullIndex int
	// use a slice because unions are usually small and
	// a linear traversal is faster then.
	choices []unionEncoderChoice
}

func (ue unionEncoder) encode(e *encodeState, v reflect.Value) {
	if v.IsNil() {
		if ue.nullIndex != -1 {
			e.writeLong(int64(ue.nullIndex))
			return
		}
		e.error(fmt.Errorf("nil value not allowed"))
	}
	v = v.Elem()
	vt := v.Type()
	for i, choice := range ue.choices {
		if choice.typ == vt {
			e.writeLong(int64(i))
			choice.enc(e, v)
			return
		}
	}
	e.error(fmt.Errorf("unknown type for union %s", vt))
}

type ptrUnionEncoder struct {
	indexes    [2]byte
	encodeElem encoderFunc
}

func (pe ptrUnionEncoder) encode(e *encodeState, v reflect.Value) {
	if v.IsNil() {
		e.writeLong(int64(pe.indexes[0]))
		return
	}
	e.writeLong(int64(pe.indexes[1]))
	pe.encodeElem(e, v.Elem())
}
