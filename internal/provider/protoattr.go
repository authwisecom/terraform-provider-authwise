package provider

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

// protoattr bridges a proto message and a Terraform object for the
// hand-written resources and data sources whose shapes tfinfra does not
// generate: messages nested several levels deep, repeated messages,
// durations, non-string maps and oneofs. The schema is derived from the
// message descriptor, so a field kit adds to the message surfaces without a
// code change here, and the conversions walk tftypes values against it.
//
// Every attribute is Optional and plain, never Computed. Reads map proto
// zero values to null, because proto3 cannot tell zero from unset; the
// resources pair that with a refresh that keeps prior state whenever it
// converts to the same proto as the server's, so an explicit zero in
// configuration (min_factors = 0) never shows up as drift.
//
// A oneof's members are attributes side by side, of which at most one may
// be set; valueToProto refuses a second. A google.protobuf.Struct has no
// schema to derive, so it is a JSON string.

var (
	durationName = (&durationpb.Duration{}).ProtoReflect().Descriptor().FullName()
	structName   = (&structpb.Struct{}).ProtoReflect().Descriptor().FullName()
)

// attrKind makes the attributes of one schema package. A resource's
// (resource/schema) and a data source's (datasource/schema) take the same
// shapes and the same conversions; only the Go types differ.
type attrKind[A any] struct {
	boolean      func(desc string) A
	int64        func(desc string) A
	float64      func(desc string) A
	str          func(desc string, validators []validator.String) A
	json         func(desc string) A
	list         func(elem attr.Type, desc string) A
	strMap       func(elem attr.Type, desc string) A
	listNested   func(attrs map[string]A, desc string) A
	singleNested func(attrs map[string]A, desc string) A
}

// protoAttributes derives resource schema attributes for the fields of md.
// descriptions is keyed by dotted field path ("floor.mode") relative to md.
func protoAttributes(md protoreflect.MessageDescriptor, descriptions map[string]string) map[string]schema.Attribute {
	return protoAttributesAt(resourceAttrs, md, "", descriptions, nil)
}

// protoDataSourceAttributes is protoAttributes for a data source schema.
// The top-level fields named in omit are left out.
func protoDataSourceAttributes(md protoreflect.MessageDescriptor, descriptions map[string]string, omit map[string]bool) map[string]dsschema.Attribute {
	return protoAttributesAt(dataSourceAttrs, md, "", descriptions, omit)
}

func protoAttributesAt[A any](k attrKind[A], md protoreflect.MessageDescriptor, prefix string, descriptions map[string]string, omit map[string]bool) map[string]A {

	attrs := map[string]A{}

	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		name := string(fd.Name())
		if omit[name] {
			continue
		}
		desc := descriptions[prefix+name]
		if od := realOneof(fd); od != nil {
			desc = withSuffix(desc, oneofNote(od))
		}
		attrs[name] = protoAttribute(k, fd, prefix+name, desc, descriptions)
	}

	return attrs
}

func protoAttribute[A any](k attrKind[A], fd protoreflect.FieldDescriptor, path, desc string, descriptions map[string]string) A {

	switch {
	case fd.IsMap():
		if fd.MapKey().Kind() != protoreflect.StringKind {
			panic(fmt.Sprintf("protoattr: %s: only string-keyed maps are supported", fd.FullName()))
		}
		return k.strMap(scalarType(fd.MapValue()), desc)
	case fd.IsList() && fd.Kind() == protoreflect.MessageKind:
		return k.listNested(protoAttributesAt(k, fd.Message(), path+".", descriptions, nil), desc)
	case fd.IsList():
		return k.list(scalarType(fd), desc)
	case isDuration(fd):
		return k.str(withSuffix(desc, "A Go duration string, e.g. `90s`, `15m`, `12h`."), []validator.String{durationValidator{}})
	case isStruct(fd):
		return k.json(withSuffix(desc, "A JSON object, e.g. from `jsonencode`."))
	case fd.Kind() == protoreflect.MessageKind:
		return k.singleNested(protoAttributesAt(k, fd.Message(), path+".", descriptions, nil), desc)
	}

	return scalarAttribute(k, fd, desc)
}

// scalarAttribute builds the attribute for a singular scalar field; enums
// are validated against their value names.
func scalarAttribute[A any](k attrKind[A], fd protoreflect.FieldDescriptor, desc string) A {

	switch scalarType(fd) {
	case types.BoolType:
		return k.boolean(desc)
	case types.Int64Type:
		return k.int64(desc)
	case types.Float64Type:
		return k.float64(desc)
	}

	var validators []validator.String
	if fd.Kind() == protoreflect.EnumKind {
		values := fd.Enum().Values()
		names := make([]string, 0, values.Len())
		for i := 0; i < values.Len(); i++ {
			names = append(names, string(values.Get(i).Name()))
		}
		validators = []validator.String{stringvalidator.OneOf(names...)}
	}
	return k.str(desc, validators)
}

// realOneof is the oneof fd belongs to, or nil for a plain field or a
// proto3 optional (whose oneof is synthetic).
func realOneof(fd protoreflect.FieldDescriptor) protoreflect.OneofDescriptor {
	if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
		return od
	}
	return nil
}

// oneofNote names the members of od for each member's description.
func oneofNote(od protoreflect.OneofDescriptor) string {
	fields := od.Fields()
	names := make([]string, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		names = append(names, "`"+string(fields.Get(i).Name())+"`")
	}
	return "At most one of " + strings.Join(names, ", ") + " may be set."
}

var resourceAttrs = attrKind[schema.Attribute]{
	boolean: func(desc string) schema.Attribute {
		return schema.BoolAttribute{Optional: true, MarkdownDescription: desc}
	},
	int64: func(desc string) schema.Attribute {
		return schema.Int64Attribute{Optional: true, MarkdownDescription: desc}
	},
	float64: func(desc string) schema.Attribute {
		return schema.Float64Attribute{Optional: true, MarkdownDescription: desc}
	},
	str: func(desc string, validators []validator.String) schema.Attribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc, Validators: validators}
	},
	json: func(desc string) schema.Attribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc, CustomType: jsontypes.NormalizedType{}}
	},
	list: func(elem attr.Type, desc string) schema.Attribute {
		return schema.ListAttribute{Optional: true, MarkdownDescription: desc, ElementType: elem}
	},
	strMap: func(elem attr.Type, desc string) schema.Attribute {
		return schema.MapAttribute{Optional: true, MarkdownDescription: desc, ElementType: elem}
	},
	listNested: func(attrs map[string]schema.Attribute, desc string) schema.Attribute {
		return schema.ListNestedAttribute{
			Optional:            true,
			MarkdownDescription: desc,
			NestedObject:        schema.NestedAttributeObject{Attributes: attrs},
		}
	},
	singleNested: func(attrs map[string]schema.Attribute, desc string) schema.Attribute {
		return schema.SingleNestedAttribute{Optional: true, MarkdownDescription: desc, Attributes: attrs}
	},
}

var dataSourceAttrs = attrKind[dsschema.Attribute]{
	boolean: func(desc string) dsschema.Attribute {
		return dsschema.BoolAttribute{Optional: true, MarkdownDescription: desc}
	},
	int64: func(desc string) dsschema.Attribute {
		return dsschema.Int64Attribute{Optional: true, MarkdownDescription: desc}
	},
	float64: func(desc string) dsschema.Attribute {
		return dsschema.Float64Attribute{Optional: true, MarkdownDescription: desc}
	},
	str: func(desc string, validators []validator.String) dsschema.Attribute {
		return dsschema.StringAttribute{Optional: true, MarkdownDescription: desc, Validators: validators}
	},
	json: func(desc string) dsschema.Attribute {
		return dsschema.StringAttribute{Optional: true, MarkdownDescription: desc, CustomType: jsontypes.NormalizedType{}}
	},
	list: func(elem attr.Type, desc string) dsschema.Attribute {
		return dsschema.ListAttribute{Optional: true, MarkdownDescription: desc, ElementType: elem}
	},
	strMap: func(elem attr.Type, desc string) dsschema.Attribute {
		return dsschema.MapAttribute{Optional: true, MarkdownDescription: desc, ElementType: elem}
	},
	listNested: func(attrs map[string]dsschema.Attribute, desc string) dsschema.Attribute {
		return dsschema.ListNestedAttribute{
			Optional:            true,
			MarkdownDescription: desc,
			NestedObject:        dsschema.NestedAttributeObject{Attributes: attrs},
		}
	},
	singleNested: func(attrs map[string]dsschema.Attribute, desc string) dsschema.Attribute {
		return dsschema.SingleNestedAttribute{Optional: true, MarkdownDescription: desc, Attributes: attrs}
	},
}

func withSuffix(desc, suffix string) string {
	if desc == "" {
		return suffix
	}
	return desc + " " + suffix
}

func isDuration(fd protoreflect.FieldDescriptor) bool {
	return fd.Kind() == protoreflect.MessageKind && fd.Message().FullName() == durationName
}

func isStruct(fd protoreflect.FieldDescriptor) bool {
	return fd.Kind() == protoreflect.MessageKind && fd.Message().FullName() == structName
}

// scalarType is the framework type of a scalar proto kind. Every integer
// width widens to Int64, and enums surface as their value names.
func scalarType(fd protoreflect.FieldDescriptor) attr.Type {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return types.BoolType
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return types.Int64Type
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return types.Float64Type
	case protoreflect.StringKind, protoreflect.EnumKind:
		return types.StringType
	default:
		panic(fmt.Sprintf("protoattr: %s: kind %s is not supported", fd.FullName(), fd.Kind()))
	}
}

// valueToProto writes the attributes of the Terraform object v onto m.
// Attributes with no field of the same name are skipped, so a resource can
// carry its own attributes (the realm it targets) beside the message's.
// Null and unknown attributes leave the field unset; a set nested object
// with nothing in it still creates the message, because presence carries
// meaning (an EnrollmentPolicy that is present is read at face value).
func valueToProto(v tftypes.Value, m protoreflect.Message) error {

	if v.IsNull() || !v.IsKnown() {
		return nil
	}

	var attrs map[string]tftypes.Value
	if err := v.As(&attrs); err != nil {
		return err
	}

	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {

		fd := fields.Get(i)
		av, ok := attrs[string(fd.Name())]
		if !ok || av.IsNull() || !av.IsKnown() {
			continue
		}

		if err := oneofFree(fd, m); err != nil {
			return err
		}

		if err := fieldToProto(fd, av, m); err != nil {
			return fmt.Errorf("%s: %w", fd.Name(), err)
		}
	}

	return nil
}

// oneofFree refuses fd when another member of its oneof is already set on
// m: the second would silently replace the first.
func oneofFree(fd protoreflect.FieldDescriptor, m protoreflect.Message) error {

	od := realOneof(fd)
	if od == nil {
		return nil
	}
	if set := m.WhichOneof(od); set != nil {
		return fmt.Errorf("%s: cannot be set with %s: %s", fd.Name(), set.Name(), oneofNote(od))
	}

	return nil
}

func fieldToProto(fd protoreflect.FieldDescriptor, v tftypes.Value, m protoreflect.Message) error {

	switch {
	case fd.IsMap():
		return mapToProto(fd, v, m.Mutable(fd).Map())
	case fd.IsList():
		return listToProto(fd, v, m.Mutable(fd).List())
	case isDuration(fd):
		var s string
		if err := v.As(&s); err != nil {
			return err
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		dp := durationpb.New(d)
		dm := m.Mutable(fd).Message()
		dfs := dm.Descriptor().Fields()
		dm.Set(dfs.ByName("seconds"), protoreflect.ValueOfInt64(dp.GetSeconds()))
		dm.Set(dfs.ByName("nanos"), protoreflect.ValueOfInt32(dp.GetNanos()))
		return nil
	case isStruct(fd):
		var s string
		if err := v.As(&s); err != nil {
			return err
		}
		return protojson.Unmarshal([]byte(s), m.Mutable(fd).Message().Interface())
	case fd.Kind() == protoreflect.MessageKind:
		return valueToProto(v, m.Mutable(fd).Message())
	default:
		pv, err := scalarToProto(fd, v)
		if err != nil {
			return err
		}
		m.Set(fd, pv)
		return nil
	}
}

func mapToProto(fd protoreflect.FieldDescriptor, v tftypes.Value, mm protoreflect.Map) error {

	var elems map[string]tftypes.Value
	if err := v.As(&elems); err != nil {
		return err
	}

	for k, ev := range elems {
		if ev.IsNull() || !ev.IsKnown() {
			continue
		}
		pv, err := scalarToProto(fd.MapValue(), ev)
		if err != nil {
			return fmt.Errorf("[%q]: %w", k, err)
		}
		mm.Set(protoreflect.ValueOfString(k).MapKey(), pv)
	}

	return nil
}

func listToProto(fd protoreflect.FieldDescriptor, v tftypes.Value, list protoreflect.List) error {

	var elems []tftypes.Value
	if err := v.As(&elems); err != nil {
		return err
	}

	for i, ev := range elems {

		if fd.Kind() == protoreflect.MessageKind {
			el := list.NewElement()
			if err := valueToProto(ev, el.Message()); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
			list.Append(el)
			continue
		}

		if ev.IsNull() || !ev.IsKnown() {
			continue
		}
		pv, err := scalarToProto(fd, ev)
		if err != nil {
			return fmt.Errorf("[%d]: %w", i, err)
		}
		list.Append(pv)
	}

	return nil
}

func scalarToProto(fd protoreflect.FieldDescriptor, v tftypes.Value) (protoreflect.Value, error) {

	switch fd.Kind() {
	case protoreflect.BoolKind:
		var b bool
		err := v.As(&b)
		return protoreflect.ValueOfBool(b), err
	case protoreflect.StringKind:
		var s string
		err := v.As(&s)
		return protoreflect.ValueOfString(s), err
	case protoreflect.EnumKind:
		var s string
		if err := v.As(&s); err != nil {
			return protoreflect.Value{}, err
		}
		ev := fd.Enum().Values().ByName(protoreflect.Name(s))
		if ev == nil {
			return protoreflect.Value{}, fmt.Errorf("%q is not a %s value", s, fd.Enum().Name())
		}
		return protoreflect.ValueOfEnum(ev.Number()), nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		var n big.Float
		if err := v.As(&n); err != nil {
			return protoreflect.Value{}, err
		}
		f, _ := n.Float64()
		if fd.Kind() == protoreflect.FloatKind {
			return protoreflect.ValueOfFloat32(float32(f)), nil
		}
		return protoreflect.ValueOfFloat64(f), nil
	default:
		return integerToProto(fd, v)
	}
}

// integerToProto narrows a Terraform number to the field's integer kind,
// refusing fractions and values the kind cannot hold.
func integerToProto(fd protoreflect.FieldDescriptor, v tftypes.Value) (protoreflect.Value, error) {

	i, err := exactInt64(v)
	if err != nil {
		return protoreflect.Value{}, err
	}

	switch fd.Kind() {
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		if i < math.MinInt32 || i > math.MaxInt32 {
			return protoreflect.Value{}, fmt.Errorf("%d is out of range for a 32-bit integer", i)
		}
		return protoreflect.ValueOfInt32(int32(i)), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		if i < 0 || i > math.MaxUint32 {
			return protoreflect.Value{}, fmt.Errorf("%d is out of range for an unsigned 32-bit integer", i)
		}
		return protoreflect.ValueOfUint32(uint32(i)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		if i < 0 {
			return protoreflect.Value{}, fmt.Errorf("%d is negative", i)
		}
		return protoreflect.ValueOfUint64(uint64(i)), nil
	default:
		return protoreflect.ValueOfInt64(i), nil
	}
}

func exactInt64(v tftypes.Value) (int64, error) {

	var n big.Float
	if err := v.As(&n); err != nil {
		return 0, err
	}
	i, acc := n.Int64()
	if acc != big.Exact {
		return 0, fmt.Errorf("%s is not an integer", n.String())
	}

	return i, nil
}

// protoToValue reads m into a Terraform object of type typ. Attributes with
// no field of the same name come back null for the caller to fill. Unset
// fields read as null: proto3 zero scalars, empty lists and maps, and
// absent messages alike.
func protoToValue(m protoreflect.Message, typ tftypes.Object) tftypes.Value {

	vals := make(map[string]tftypes.Value, len(typ.AttributeTypes))
	fields := m.Descriptor().Fields()

	for name, at := range typ.AttributeTypes {
		fd := fields.ByName(protoreflect.Name(name))
		if fd == nil || !m.Has(fd) {
			vals[name] = tftypes.NewValue(at, nil)
			continue
		}
		vals[name] = fieldToValue(fd, m.Get(fd), at)
	}

	return tftypes.NewValue(typ, vals)
}

func fieldToValue(fd protoreflect.FieldDescriptor, pv protoreflect.Value, typ tftypes.Type) tftypes.Value {

	switch {
	case fd.IsMap():
		elems := map[string]tftypes.Value{}
		pv.Map().Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
			elems[k.String()] = scalarToValue(fd.MapValue(), v)
			return true
		})
		return tftypes.NewValue(typ, elems)
	case fd.IsList():
		et := typ.(tftypes.List).ElementType
		list := pv.List()
		elems := make([]tftypes.Value, 0, list.Len())
		for i := 0; i < list.Len(); i++ {
			if fd.Kind() == protoreflect.MessageKind {
				elems = append(elems, protoToValue(list.Get(i).Message(), et.(tftypes.Object)))
				continue
			}
			elems = append(elems, scalarToValue(fd, list.Get(i)))
		}
		return tftypes.NewValue(typ, elems)
	case isDuration(fd):
		dm := pv.Message()
		dfs := dm.Descriptor().Fields()
		d := time.Duration(dm.Get(dfs.ByName("seconds")).Int())*time.Second +
			time.Duration(dm.Get(dfs.ByName("nanos")).Int())
		return tftypes.NewValue(tftypes.String, d.String())
	case isStruct(fd):
		b, err := protojson.Marshal(pv.Message().Interface())
		if err != nil {
			// A Struct holds only JSON values, so it always encodes.
			panic(fmt.Sprintf("protoattr: %s: %v", fd.FullName(), err))
		}
		return tftypes.NewValue(tftypes.String, string(b))
	case fd.Kind() == protoreflect.MessageKind:
		return protoToValue(pv.Message(), typ.(tftypes.Object))
	default:
		return scalarToValue(fd, pv)
	}
}

func scalarToValue(fd protoreflect.FieldDescriptor, pv protoreflect.Value) tftypes.Value {

	switch fd.Kind() {
	case protoreflect.BoolKind:
		return tftypes.NewValue(tftypes.Bool, pv.Bool())
	case protoreflect.StringKind:
		return tftypes.NewValue(tftypes.String, pv.String())
	case protoreflect.EnumKind:
		name := fmt.Sprint(int32(pv.Enum()))
		if ev := fd.Enum().Values().ByNumber(pv.Enum()); ev != nil {
			name = string(ev.Name())
		}
		return tftypes.NewValue(tftypes.String, name)
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return tftypes.NewValue(tftypes.Number, big.NewFloat(pv.Float()))
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return tftypes.NewValue(tftypes.Number, new(big.Float).SetUint64(pv.Uint()))
	default:
		return tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(pv.Int()))
	}
}

// durationValidator refuses a string time.ParseDuration cannot read, at
// plan time rather than at apply.
type durationValidator struct{}

func (durationValidator) Description(context.Context) string {
	return "must be a Go duration string"
}

func (v durationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (durationValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := time.ParseDuration(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "invalid duration", err.Error())
	}
}
