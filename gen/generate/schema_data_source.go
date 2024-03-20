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
)

type schemaDataSourceHandler struct {
	file          *j.File
	schemas       map[protoreflect.Name]bool
	packageImport protogen.GoImportPath
	filename      string
}

func (s *schemaDataSourceHandler) Init(p *protogen.Plugin) {
	s.file = j.NewFile(PackageName)
}

func (s *schemaDataSourceHandler) Finish(p *protogen.Plugin) {

	gf := p.NewGeneratedFile(s.filename, s.packageImport)
	PrintGeneratedHeader(gf)
	gf.P(s.file.GoString())
}

func (s *schemaDataSourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "Schema").Str("proto", m.GoIdent.GoName).Logger()
	name := m.Desc.Name()
	if _, exists := s.schemas[name]; exists {
		l.Debug().Msg("Schema exists")
		return nil
	}
	l.Debug().Msg("Generating")
	s.schemas[name] = true
	s.schema(m)
	l.Debug().Msg("Finished generating")
	return nil
}

type SchemaDataSourceHandlerParams struct {
	PackageName string
	Filename    string
}

func NewSchemaDataSourceHandler(params SchemaDataSourceHandlerParams) MessageHandler {

	return &schemaDataSourceHandler{
		schemas:       map[protoreflect.Name]bool{},
		packageImport: protogen.GoImportPath(params.PackageName),
		filename:      params.Filename,
	}
}

func (s *schemaDataSourceHandler) schema(m *protogen.Message) {
	id := "GenSchema" + m.GoIdent.GoName + "DataSource"
	l := log.With().Str("generator", "Schema").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating schema")
	s.file.Commentf("// %v returns tfsdk.Schema definition for %v\n", id, m.GoIdent.GoName).
		Func().Id(id).Params(j.Id("ctx").Qual("context", "Context")).
		Params(j.Qual(DataSourceSchema, "Schema")).Block(
		j.Return(j.Qual(DataSourceSchema, "Schema").Values(j.Dict{
			j.Id("Attributes"): j.Map(j.String()).Qual(DataSourceSchema, "Attribute").Values(
				fieldsDataSource(l, m),
			),
		})),
	)
}

func fieldsDataSource(l zerolog.Logger, m *protogen.Message) j.Dict {
	cfg := loadConfig(m)
	d := j.Dict{}

	injected := map[string]bool{}

	for k, _ := range cfg.InjectedFields {
		injected[k] = true
	}

	l.Debug().Msgf("test: %v", m.GoIdent.GoName)
	meta := resourceMetadataMap[m.GoIdent.GoName]
	if meta != nil {
		s := meta.schemaMetadata
		if s != nil {
			//append computed result field to schema
			if s.hasResult {
				d[j.Lit("result")] = j.Qual(DataSourceSchema, "StringAttribute").Values(
					j.Dict{
						j.Id("CustomType"):  j.Qual(JSONTypes, "NormalizedType").Values(),
						j.Id("Description"): j.Lit(""),
						j.Id("Computed"):    j.True(),
					},
				)
			}
		}
	}

	for _, f := range m.Fields {

		name := strcase.ToSnake(f.GoName)

		if injected[name] {
			continue
		}

		// Hack to handle structs
		if f.Parent.Desc.FullName() == "google.protobuf.Struct" {
			d[j.Lit(name)] = j.Qual(DataSourceSchema, "SingleNestedAttribute").Values(j.Dict{
				j.Id("Description"): j.Lit(trimComments(f.Comments.Leading)),
			})
			continue
		}
		d[j.Lit(name)] = fieldDataSource(l, f)
	}

	return d
}

func fieldDataSource(l zerolog.Logger, f *protogen.Field) j.Code {

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
			d[j.Id("NestedObject")] = j.Qual(DataSourceSchema, "NestedAttributeObject").Values(j.Dict{
				j.Id("Attributes"): j.Map(j.String()).Qual(DataSourceSchema, "Attribute").Values(
					fieldsDataSource(l, f.Message),
				),
			})
			return j.Qual(DataSourceSchema, "MapNestedAttribute").Values(d)
		} else {
			typeTypeName = typeTypeMap[f.Desc.MapValue().Kind()]
			if typeTypeName == "" {
				panic("unhandled kind " + f.Desc.MapValue().Kind().String())
			}
			d[j.Id("ElementType")] = j.Qual(Types, typeTypeName)
			return j.Qual(DataSourceSchema, "MapAttribute").Values(d)
		}
	} else if f.Desc.IsList() {
		if f.Message != nil {
			d[j.Id("NestedObject")] = j.Qual(DataSourceSchema, "NestedAttributeObject").Values(j.Dict{
				j.Id("Attributes"): j.Map(j.String()).Qual(DataSourceSchema, "Attribute").Values(
					fieldsDataSource(l, f.Message),
				),
			})
			return j.Qual(DataSourceSchema, "ListNestedAttribute").Values(d)
		} else {
			if typeTypeName == "" {
				panic("unhandled kind " + f.Desc.Kind().String())
			}
			d[j.Id("ElementType")] = j.Qual(Types, typeTypeName)
			return j.Qual(DataSourceSchema, "ListAttribute").Values(d)
		}
	} else if f.Message != nil {
		d[j.Id("Attributes")] = j.Map(j.String()).Qual(DataSourceSchema, "Attribute").Values(
			fieldsDataSource(l, f.Message),
		)
		return j.Qual(DataSourceSchema, "SingleNestedAttribute").Values(d)
	} else {
		if attributeTypeName == "" {
			panic("unhandled kind " + f.Desc.Kind().String())
		}
		return j.Qual(DataSourceSchema, attributeTypeName).Values(d)
	}

}
