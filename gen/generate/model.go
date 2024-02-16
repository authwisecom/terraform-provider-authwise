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
	"google.golang.org/protobuf/compiler/protogen"
)

func Model(f *j.File, m *protogen.Message) {
	id := m.GoIdent.GoName + "Model"
	l := log.With().Str("generator", "Model").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating model")
	f.Commentf("// %v model\n", id).
		Type().Id(id).Struct(modelFields(l, m)...)

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

func modelFields(l zerolog.Logger, m *protogen.Message) []j.Code {
	//cfg := loadConfig(m)
	var c []j.Code
	for _, f := range m.Fields {

		// Hack to handle structs
		if f.Parent.Desc.FullName() == "google.protobuf.Struct" {
			c = append(c, j.Id(f.GoName).Map(j.String()).Any())
			continue
		}

		c = append(c, modelField(l, f))
	}

	/*
		for key, value := range cfg.InjectedFields {
			// TODO snake case name
			d[j.Lit(strcase.ToSnake(key))] = generateInjectedField(l, value)
		}

	*/

	return c
}

func modelField(l zerolog.Logger, f *protogen.Field) j.Code {

	l.Debug().Msgf("handling field: %v", f.GoName)

	typeName := typeMap[f.Desc.Kind()]

	var result *j.Statement

	if f.Desc.IsMap() {
		val := f.Desc.MapValue()
		if val.Message() != nil {
			result = j.Id(f.GoName).Qual(Types, "MapType")
		} else {
			typeName = typeMap[f.Desc.MapValue().Kind()]
			if typeName == "" {
				panic("unhandled kind " + f.Desc.MapValue().Kind().String())
			}
			result = j.Id(f.GoName).Qual(Types, "MapType")
		}
	} else if f.Desc.IsList() {
		if f.Message != nil {
			result = j.Id(f.GoName).Qual(Types, "ListType")
		} else {
			if typeName == "" {
				panic("unhandled kind " + f.Desc.Kind().String())
			}
			result = j.Id(f.GoName).Qual(Types, "ListType")
		}
	} else if f.Message != nil {
		result = j.Id(f.GoName).Qual(Types, "ObjectType")
	} else {
		if typeName == "" {
			panic("unhandled kind " + f.Desc.Kind().String())
		}
		result = j.Id(f.GoName).Qual(Types, typeName)
	}

	result.Tag(map[string]string{
		"tfsdk": strcase.ToSnake(string(f.Desc.Name())),
	})

	return result

}

func modelGetInjectedFields(l zerolog.Logger, f injectedField) j.Code {

	return j.Qual(ResourceSchema, f.Type).Values(j.Dict{
		j.Id("Required"): j.Lit(f.Required),
		j.Id("Computed"): j.Lit(f.Computed),
		j.Id("Optional"): j.Lit(f.Optional),
	})

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
