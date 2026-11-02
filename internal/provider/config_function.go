package provider

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var _ function.Function = &configJSONFunction{}

// configJSONFunction is a config data source as a provider-defined function
// (#55): provider::authwise::issuer_config({...}) returns what
// data.authwise_issuer_config's `json` holds for the same arguments.
//
// It exists because Terraform reads a data source at apply whenever a
// resource named in its arguments has a change planned, even when every
// argument is known, so the resource taking `json` plans an update that
// applies as a no-op. A function is evaluated wherever its arguments are
// known, plan included, and is never deferred.
//
// The argument is one object written like the data source's arguments. It
// is dynamic, because an object parameter would need every attribute
// written out; conformConfig checks it against the data source's schema
// instead, so the attribute names, types and checks are the data source's.
type configJSONFunction struct {
	spec configJSONSpec
}

func newConfigJSONFunction(spec configJSONSpec) func() function.Function {
	return func() function.Function { return &configJSONFunction{spec: spec} }
}

func configJSONFunctions() []func() function.Function {
	out := make([]func() function.Function, 0, len(configJSONSpecs))
	for _, s := range configJSONSpecs {
		out = append(out, newConfigJSONFunction(s))
	}
	return out
}

func (f *configJSONFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = f.spec.typeName
}

func (f *configJSONFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {

	name := f.spec.message.ProtoReflect().Descriptor().Name()

	resp.Definition = function.Definition{
		Summary: fmt.Sprintf("Builds the protojson of %s, for `%s`", name, f.spec.attribute),
		MarkdownDescription: fmt.Sprintf(
			"Builds the %s message from an object and returns its protojson encoding, the value `%s` takes: "+
				"the same string `data.authwise_%s` exposes as `json` for the same arguments, with the same attributes and checks. Requires Terraform 1.8 or later.\n\n"+
				"Use it in place of the data source to keep plans quiet. Terraform reads a data source at apply whenever a resource named in its arguments has a change planned, "+
				"so `%s` plans an update that applies as a no-op; this function is evaluated at plan whenever its argument is known.",
			name, f.spec.attribute, f.spec.typeName, f.spec.attribute),
		Parameters: []function.Parameter{
			function.DynamicParameter{
				Name: "config",
				MarkdownDescription: fmt.Sprintf("An object with the attributes of `data.authwise_%s`, snake_case as there. "+
					"Unset attributes are left out.", f.spec.typeName),
			},
		},
		Return: function.StringReturn{},
	}
}

func (f *configJSONFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {

	var arg types.Dynamic
	resp.Error = function.ConcatFuncErrors(resp.Error, req.Arguments.Get(ctx, &arg))
	if resp.Error != nil {
		return
	}

	out, err := f.encode(ctx, arg)
	if err != nil {
		resp.Error = err
		return
	}

	resp.Error = function.ConcatFuncErrors(resp.Error, resp.Result.Set(ctx, out))
}

// encode is the argument's protojson, or unknown while any of it is.
func (f *configJSONFunction) encode(ctx context.Context, arg types.Dynamic) (types.String, *function.FuncError) {

	if arg.IsUnknown() || arg.IsUnderlyingValueUnknown() {
		return types.StringUnknown(), nil
	}

	config, err := f.conform(ctx, arg)
	if err != nil {
		return types.String{}, function.NewArgumentFuncError(0, err.Error())
	}
	if !config.IsFullyKnown() {
		return types.StringUnknown(), nil
	}

	if r := f.spec.reservedViolations(config); len(r) > 0 {
		return types.String{}, function.NewArgumentFuncError(0, fmt.Sprintf("%s: not yet supported: %s", r[0].path, r[0].detail))
	}

	m := f.spec.message.ProtoReflect().New()
	if err := valueToProto(config, m); err != nil {
		return types.String{}, function.NewArgumentFuncError(0, fmt.Sprintf("invalid %s: %s", m.Descriptor().Name(), err))
	}

	doc, err := configJSON(m.Interface())
	if err != nil {
		return types.String{}, function.NewFuncError(fmt.Sprintf("cannot encode %s: %s", m.Descriptor().Name(), err))
	}

	return types.StringValue(doc), nil
}

// conform is the argument as the data source's arguments: an object of
// its type, unset attributes null.
func (f *configJSONFunction) conform(ctx context.Context, arg types.Dynamic) (tftypes.Value, error) {

	in := tftypes.NewValue(tftypes.DynamicPseudoType, nil)
	if !arg.IsNull() && !arg.IsUnderlyingValueNull() {
		v, err := arg.UnderlyingValue().ToTerraformValue(ctx)
		if err != nil {
			return tftypes.Value{}, err
		}
		in = v
	}

	return conformConfig(in, f.objectType(ctx), "")
}

// objectType is the data source's arguments as one object type.
func (f *configJSONFunction) objectType(ctx context.Context) tftypes.Object {
	md := f.spec.message.ProtoReflect().Descriptor()
	attrs := protoDataSourceAttributes(md, nil, f.spec.omit)
	return dsschema.Schema{Attributes: attrs}.Type().TerraformType(ctx).(tftypes.Object)
}

// conformConfig converts v, the value written for the argument, to typ: an
// object literal to the object with its unset attributes null, a tuple to a
// list, an object to a map. An attribute typ does not have is refused, as a
// data source refuses an unsupported argument; so is a value of another
// type. at is the path so far, for the error.
func conformConfig(v tftypes.Value, typ tftypes.Type, at string) (tftypes.Value, error) {

	if v.IsNull() {
		return tftypes.NewValue(typ, nil), nil
	}
	if !v.IsKnown() {
		return tftypes.NewValue(typ, tftypes.UnknownValue), nil
	}

	switch t := typ.(type) {
	case tftypes.Object:
		return conformObject(v, t, at)
	case tftypes.List:
		return conformList(v, t, at)
	case tftypes.Map:
		return conformMap(v, t, at)
	}

	if !v.Type().Equal(typ) {
		return tftypes.Value{}, fmt.Errorf("%s: must be a %s, not a %s", where(at), friendlyType(typ), friendlyType(v.Type()))
	}

	return v, nil
}

func conformObject(v tftypes.Value, t tftypes.Object, at string) (tftypes.Value, error) {

	if !v.Type().Is(tftypes.Object{}) && !v.Type().Is(tftypes.Map{}) {
		return tftypes.Value{}, fmt.Errorf("%s: must be an object, not a %s", where(at), friendlyType(v.Type()))
	}

	var in map[string]tftypes.Value
	if err := v.As(&in); err != nil {
		return tftypes.Value{}, err
	}

	for _, name := range slices.Sorted(maps.Keys(in)) {
		if _, ok := t.AttributeTypes[name]; !ok {
			return tftypes.Value{}, fmt.Errorf("%s: unsupported attribute %q", where(at), name)
		}
	}

	out := make(map[string]tftypes.Value, len(t.AttributeTypes))
	for name, at2 := range t.AttributeTypes {
		ev, ok := in[name]
		if !ok {
			out[name] = tftypes.NewValue(at2, nil)
			continue
		}
		cv, err := conformConfig(ev, at2, join(at, name))
		if err != nil {
			return tftypes.Value{}, err
		}
		out[name] = cv
	}

	return tftypes.NewValue(t, out), nil
}

func conformList(v tftypes.Value, t tftypes.List, at string) (tftypes.Value, error) {

	if !v.Type().Is(tftypes.Tuple{}) && !v.Type().Is(tftypes.List{}) && !v.Type().Is(tftypes.Set{}) {
		return tftypes.Value{}, fmt.Errorf("%s: must be a list, not a %s", where(at), friendlyType(v.Type()))
	}

	var in []tftypes.Value
	if err := v.As(&in); err != nil {
		return tftypes.Value{}, err
	}

	out := make([]tftypes.Value, 0, len(in))
	for i, ev := range in {
		cv, err := conformConfig(ev, t.ElementType, fmt.Sprintf("%s[%d]", at, i))
		if err != nil {
			return tftypes.Value{}, err
		}
		out = append(out, cv)
	}

	return tftypes.NewValue(t, out), nil
}

func conformMap(v tftypes.Value, t tftypes.Map, at string) (tftypes.Value, error) {

	if !v.Type().Is(tftypes.Object{}) && !v.Type().Is(tftypes.Map{}) {
		return tftypes.Value{}, fmt.Errorf("%s: must be a map, not a %s", where(at), friendlyType(v.Type()))
	}

	var in map[string]tftypes.Value
	if err := v.As(&in); err != nil {
		return tftypes.Value{}, err
	}

	out := make(map[string]tftypes.Value, len(in))
	for k, ev := range in {
		cv, err := conformConfig(ev, t.ElementType, fmt.Sprintf("%s[%q]", at, k))
		if err != nil {
			return tftypes.Value{}, err
		}
		out[k] = cv
	}

	return tftypes.NewValue(t, out), nil
}

func join(at, name string) string {
	if at == "" {
		return name
	}
	return at + "." + name
}

func where(at string) string {
	if at == "" {
		return "config"
	}
	return at
}

// friendlyType names a type as a practitioner writes it.
func friendlyType(t tftypes.Type) string {
	switch {
	case t.Is(tftypes.String):
		return "string"
	case t.Is(tftypes.Number):
		return "number"
	case t.Is(tftypes.Bool):
		return "bool"
	case t.Is(tftypes.Object{}), t.Is(tftypes.Map{}):
		return "object"
	case t.Is(tftypes.Tuple{}), t.Is(tftypes.List{}), t.Is(tftypes.Set{}):
		return "list"
	}
	return strings.ToLower(t.String())
}
