// Copyright 2022 Liam White
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package generate

import (
	j "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"regexp"
	"strings"
)

func Schema(f *j.File, m *protogen.Message) {
	id := "GenSchema" + m.GoIdent.GoName
	l := log.With().Str("generator", "Schema").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating schema")
	f.Commentf("// %v returns tfsdk.Schema definition for %v\n", id, m.GoIdent.GoName).
		Func().Id(id).Params(j.Id("ctx").Qual("context", "Context")).
		Params(j.Qual(ResourceSchema, "Schema")).Block(
		j.Return(j.Qual(ResourceSchema, "Schema").Values(j.Dict{
			j.Id("Attributes"): j.Map(j.String()).Qual(ResourceSchema, "Attribute").Values(
				fields(l, m),
			),
		})),
	)
	/*
		l.Debug().Msg("Generating schema")
		f.Commentf("// %v returns tfsdk.Schema definition for %v\n", id, m.GoIdent.GoName).
			Func().
			Id(id).

			Params(j.Id("ctx").Qual("context", "Context")).
			Params(j.Qual(SDK, "Schema"), j.Qual(Diag, "Diagnostics")).
			Block(j.Return(
				j.Qual(SDK, "Schema").Values(j.Dict{
					j.Id("Attributes"): j.Map(j.String()).Qual(SDK, "Attribute").Values(
						fieldsDictSchema(l, m),
					),
				}),
				j.Nil(),
			))
	*/
}

func fields(l zerolog.Logger, m *protogen.Message) j.Dict {
	cfg := loadConfig(m)
	d := j.Dict{}

	injected := map[string]bool{}

	for k, _ := range cfg.InjectedFields {
		injected[k] = true
	}

	for _, f := range m.Fields {

		name := strcase.ToSnake(f.GoName)

		if injected[name] {
			continue
		}

		// Hack to handle structs
		if f.Parent.Desc.FullName() == "google.protobuf.Struct" {
			d[j.Lit(name)] = j.Qual(ResourceSchema, "MapAttribute").Values(j.Dict{
				j.Id("Description"): j.Lit(trimComments(f.Comments.Leading)),
			})
			continue
		}

		d[j.Lit(name)] = field(l, f)
	}

	for key, value := range cfg.InjectedFields {
		// TODO snake case name
		d[j.Lit(strcase.ToSnake(key))] = generateInjectedField(l, value)
	}

	return d
}

var attributeTypeMap = map[protoreflect.Kind]string{
	protoreflect.StringKind: "StringAttribute",
	protoreflect.BytesKind:  "StringAttribute",
	protoreflect.Int32Kind:  "Int64Attribute",
	protoreflect.Int64Kind:  "Int64Attribute",
	protoreflect.EnumKind:   "Int64Attribute",
	protoreflect.FloatKind:  "Float64Attribute",
	protoreflect.DoubleKind: "Float64Attribute",
	protoreflect.BoolKind:   "BoolAttribute",
}

func field(l zerolog.Logger, f *protogen.Field) j.Code {

	l.Debug().Msgf("handling field: %v", f.GoName)

	d := j.Dict{
		j.Id("Description"): j.Lit(trimComments(f.Comments.Leading)),
	}

	// Handle field behavior annotations
	opts := f.Desc.Options().(*descriptorpb.FieldOptions)
	optional := true
	for _, b := range proto.GetExtension(opts, annotations.E_FieldBehavior).([]annotations.FieldBehavior) {
		switch b {
		case annotations.FieldBehavior_REQUIRED:
			d[j.Id("Required")] = j.Lit(true)
			optional = false
		}
	}
	// If required or computed is not set, default to optional
	if optional {
		d[j.Id("Optional")] = j.Lit(true)
	}

	attributeTypeName := attributeTypeMap[f.Desc.Kind()]
	typeTypeName := typeTypeMap[f.Desc.Kind()]

	if f.Desc.IsMap() {
		val := f.Desc.MapValue()
		if val.Message() != nil {
			d[j.Id("NestedObject")] = j.Qual(ResourceSchema, "NestedAttributeObject").Values(j.Dict{
				j.Id("Attributes"): j.Map(j.String()).Qual(ResourceSchema, "Attribute").Values(
					fields(l, f.Message),
				),
			})
			return j.Qual(ResourceSchema, "MapNestedAttribute").Values(d)
		} else {
			typeTypeName = typeTypeMap[f.Desc.MapValue().Kind()]
			if typeTypeName == "" {
				panic("unhandled kind " + f.Desc.MapValue().Kind().String())
			}
			d[j.Id("ElementType")] = j.Qual(Types, typeTypeName)
			return j.Qual(ResourceSchema, "MapAttribute").Values(d)
		}
	} else if f.Desc.IsList() {
		if f.Message != nil {
			d[j.Id("NestedObject")] = j.Qual(ResourceSchema, "NestedAttributeObject").Values(j.Dict{
				j.Id("Attributes"): j.Map(j.String()).Qual(ResourceSchema, "Attribute").Values(
					fields(l, f.Message),
				),
			})
			return j.Qual(ResourceSchema, "ListNestedAttribute").Values(d)
		} else {
			if typeTypeName == "" {
				panic("unhandled kind " + f.Desc.Kind().String())
			}
			d[j.Id("ElementType")] = j.Qual(Types, typeTypeName)
			return j.Qual(ResourceSchema, "ListAttribute").Values(d)
		}
	} else if f.Message != nil {
		d[j.Id("Attributes")] = j.Map(j.String()).Qual(ResourceSchema, "Attribute").Values(
			fields(l, f.Message),
		)
		return j.Qual(ResourceSchema, "SingleNestedAttribute").Values(d)
	} else {
		if attributeTypeName == "" {
			panic("unhandled kind " + f.Desc.Kind().String())
		}
		return j.Qual(ResourceSchema, attributeTypeName).Values(d)
	}

}

func generateInjectedField(l zerolog.Logger, f injectedField) j.Code {

	d := j.Dict{
		j.Id("Required"): j.Lit(f.Required),
		j.Id("Computed"): j.Lit(f.Computed),
		j.Id("Optional"): j.Lit(f.Optional),
	}

	// TODO - we need to check the type for this
	if f.PlanModifierUseStateForUnknown {
		d[j.Id("PlanModifiers")] = j.Index().Qual(ResourcePlanModifier, "String").Values(
			j.Qual(ResourceStringPlanModifier, "UseStateForUnknown").Call())
	}

	return j.Qual(ResourceSchema, f.Type).Values(d)

}

/*
func schemaType(l zerolog.Logger, d protoreflect.FieldDescriptor) *j.Statement {
	if d.IsList() {
		// If the type isnt a primitive then type is nil, we use attributes instead.
		if _, ok := primitiveTypeMap[d.Kind()]; !ok {
			return nil
		}
		return j.Qual(Types, "ListType").Values(j.Dict{
			j.Id("ElemType"): primitiveTypeMap[d.Kind()],
		})
	}
	if d.IsMap() {
		// If the type isnt a primitive then type is nil, we use attributes instead.
		if _, ok := primitiveTypeMap[d.MapValue().Kind()]; !ok {
			return nil
		}
		return j.Qual(Types, "MapType").Values(j.Dict{
			j.Id("ElemType"): primitiveTypeMap[d.MapValue().Kind()],
		})
	}
	return primitiveTypeMap[d.Kind()]

	return nil
}


func attributes(l zerolog.Logger, f *protogen.Field) *j.Statement {
	// If message is not nil it can't be a primitive type (string, bool, etc.).
	if f.Message == nil {
		panic("attributes requires a message")
	}
	if f.Desc.IsList() {
		return xNestAttributes(l, "List", f.Message)
	}
	if f.Desc.IsMap() {
		// If the map has a primitive value we use type, not attributes.
		// TODO - fix
		if _, ok := attributeTypeMap[f.Desc.MapValue().Kind()]; ok {
			return nil
		}
		// Not sure how safe the assumption that fields[1] is always value and not key ¯\_(ツ)_/¯.
		return xNestAttributes(l, "Map", f.Message.Fields[1].Message)
	}
	// If we've got this far is must be single nested
	return xNestAttributes(l, "Single", f.Message)

}

func xNestAttributes(l zerolog.Logger, typ string, m *protogen.Message) *j.Statement {
	return j.Qual(ResourceSchema, typ+"NestedAttributes").Params(
		j.Map(j.String()).Qual(ResourceSchema, "Attribute").Values(fieldsDictSchema(l, m)),
	)
}
*/

var (
	newlinePattern  = regexp.MustCompile(`\n//`)
	variablePattern = regexp.MustCompile(`[ ]*\$[^\/]+[ ]*`)
)

func trimComments(c protogen.Comments) string {

	trimmed := strings.TrimSpace(strings.TrimPrefix(c.String(), "// "))
	trimmed = newlinePattern.ReplaceAllString(trimmed, "")
	trimmed = variablePattern.ReplaceAllString(trimmed, "")

	return trimmed
}

/*
func snakeCase(s string) string {
	matchFirstCap := regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap := regexp.MustCompile("([a-z0-9])([A-Z])")
	snake := matchFirstCap.ReplaceAllString(s, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

*/

// func handleStructValue()
