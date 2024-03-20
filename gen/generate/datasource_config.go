package generate

import (
	"fmt"
	j "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"strings"
)

type configDataSourceHandler struct {
	plugin        *protogen.Plugin
	packageImport protogen.GoImportPath
	schemaHandler MessageHandler
}

func (s *configDataSourceHandler) Init(p *protogen.Plugin) {
	s.plugin = p
}

func (s *configDataSourceHandler) Finish(p *protogen.Plugin) {
}

func (s *configDataSourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "ConfigDataSource").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating")
	err := s.schemaHandler.Handle(m)
	if err != nil {
		return err
	}

	snakeName := strcase.ToSnake(string(m.Desc.Name()))

	if err = s.generateDataSource(m, snakeName); err != nil {
		return err
	}

	l.Debug().Msg("Finished generating")
	return nil
}

func (s *configDataSourceHandler) generateDataSource(m *protogen.Message, name string) error {
	l := log.With().Str("generator", "ConfigDataSource").Str("proto", m.GoIdent.GoName).Logger()
	f := j.NewFile(PackageName)
	s.datasource(f, m, l)
	gf := s.plugin.NewGeneratedFile(fmt.Sprintf("config_%s_data_source.go", name), s.packageImport)
	PrintGeneratedHeader(gf)
	if _, err := gf.Write([]byte(f.GoString())); err != nil {
		return err
	}
	return nil
}

func (s *configDataSourceHandler) datasource(f *j.File, m *protogen.Message, l zerolog.Logger) {

	name := m.GoIdent.GoName
	structName := strcase.ToLowerCamel(name) + "DataSource"

	f.Type().Id(structName).Struct()

	f.Func().Id(fmt.Sprintf("New%s", name)).Params().Qual(DataSource, "DataSource").Block(
		j.Return(j.Op("&").Id(structName).Values()),
	).Line()

	s.model(f, m, l)
	s.computeResult(f, m, structName, l)
	s.metadata(f, m, structName, l)
	s.schema(f, m, structName, resourceMetadataMap, l)
	s.read(f, m, structName, l)

}

func (s *configDataSourceHandler) requestResponseMethod(f *j.File,
	structName, functionName string,
	statements ...j.Code) {

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id(functionName).Params(
		j.Id("ctx").Qual("context", "Context"),
		j.Id("request").Qual(DataSource, fmt.Sprintf("%sRequest", functionName)),
		j.Id("response").Op("*").Qual(DataSource, fmt.Sprintf("%sResponse", functionName)),
	).Block(statements...).Line()
}

func (s *configDataSourceHandler) crudMethodTemplate(m *protogen.Message, body ...j.Code) []j.Code {
	name := m.GoIdent.GoName
	requestObjName := "Config"

	//standard crud method start - config only read
	result := []j.Code{
		j.Var().Id("data").Qual("", s.dataModelName(m)).Line(),
		j.Id("response").Dot("Diagnostics").Dot("Append").Call(
			j.Id("request").Dot(requestObjName).Dot("Get").Call(j.Id("ctx"), j.Op("&").Id("data")).Op("..."),
		).Line(),
		j.If(j.Id("response").Dot("Diagnostics").Dot("HasError").Call().Block(
			j.Return(),
		).Line()),
		j.Add(body...),
		j.Qual(TFLog, "Trace").Call(
			j.Id("ctx"),
			j.Lit(fmt.Sprintf("%s %s", strings.ToLower(name), "read")),
		).Line(),
		j.Id("response").Dot("Diagnostics").Dot("Append").Call(
			j.Id("response").Dot("State").Dot("Set").Call(j.Id("ctx"), j.Id("data")).Op("..."),
		),
	}

	return result
}

func (s *configDataSourceHandler) dataModelName(m *protogen.Message) string {
	return strcase.ToLowerCamel(m.GoIdent.GoName) + "DataSourceModel"
}

func (s *configDataSourceHandler) modelFields(fields []*protogen.Field, l zerolog.Logger) []j.Code {
	var result []j.Code
	for _, f := range fields {
		typeName := "Object"
		schemaName := strcase.ToSnake(f.GoName)
		// TODO - Build out this set of data
		switch f.Desc.Kind() {
		case protoreflect.StringKind:
			if f.Desc.IsList() {
				typeName = "List"
			} else {
				typeName = "String"
			}
		case protoreflect.BoolKind:
			typeName = "Bool"
		case protoreflect.Int64Kind, protoreflect.Int32Kind:
			typeName = "Int64"
		case protoreflect.MessageKind:
			typeName = "Object"
		default:
			panic("unknown type")
		}
		log.Debug().Str("kind", f.Desc.Kind().GoString()).Msg("processing model field")
		result = append(result, j.Id(f.GoName).Qual(Types, typeName).Tag(map[string]string{
			"tfsdk": schemaName,
		}))
	}
	//append type field
	result = append(result, j.Id("Result").Qual(JSONTypes, "Normalized").Tag(map[string]string{
		"tfsdk": "result",
	}))
	return result
}

func (s *configDataSourceHandler) computeResult(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {

	//converted fields
	var conversions []j.Code
	errCheck := j.If(j.Id("err").Op("!=").Nil().Block(j.Return(j.Nil(), j.Id("err")))).Line()
	for _, v := range m.Fields {
		tfName := fmt.Sprintf("%s", v.Desc.FullName().Name())
		//only list/struct/map fields
		if v.Desc.IsList() {
			if v.Desc.Kind() == protoreflect.MessageKind {
				code := j.Commentf("TODO - %s unsupported list type", v.GoName).Line().
					Id(ConversionName(strings.ToLower(tfName))).Op(":=").Id("map[string]any{}")
				conversions = append(conversions, code)
			} else {
				code := j.List(
					j.Id(ConversionName(strings.ToLower(tfName))),
					j.Id("err")).Op(":=").Qual(Util, "ListToSlice").
					Types(j.Id(v.Desc.Kind().String())).Call(j.Id("m").Dot(v.GoName)).Line().Add(errCheck)
				conversions = append(conversions, code)
			}
		} else if v.Desc.IsMap() {
			panic("computeResult: unimplemented")
		} else if v.Desc.Kind() == protoreflect.MessageKind {
			//concrete
			code := j.List(
				j.Id(ConversionName(strings.ToLower(tfName))),
				j.Id("err")).Op(":=").Qual(Util, "ObjectToMap").
				Call(j.Id("m").Dot(v.GoName)).Line().Add(errCheck)
			conversions = append(conversions, code)
		}
	}

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("computeResult").Params(j.List(
		j.Id("ctx").Qual("context", "Context"),
		j.Id("m").Op("*").Id(s.dataModelName(m))),
	).Parens(j.List(j.Op("*").Qual(JSONTypes, "Normalized"), j.Id("error"))).Block(
		j.Add(conversions...).Line().Id("converted").Op(":=").Map(j.String()).Any().Values(
			//map assignment
			j.DictFunc(func(d j.Dict) {
				//add type field
				d[j.Lit("@type")] = j.Lit(fmt.Sprintf("type.googleapis.com/%s", m.Desc.FullName()))
				for _, v := range m.Fields {
					tfName := fmt.Sprintf("%s", v.Desc.FullName().Name())
					if tfName == "result" {
						continue
					}
					t, ok := valueTypeMap[v.Desc.Kind()]
					if ok && !v.Desc.IsList() && !v.Desc.IsMap() {
						//standard
						d[j.Lit(tfName)] = j.Id("m").Dot(v.GoName).Dot(t).Call()
					} else {
						//converted object/list
						d[j.Lit(tfName)] = j.Id(ConversionName(strings.ToLower(tfName)))
					}
				}
			}),
		),
		j.List(j.Id("j"), j.Id("err")).Op(":=").Qual("encoding/json", "Marshal").Call(j.Id("converted")).Line(),
		j.If(j.Id("err").Op("!=").Nil()).Block(
			j.Return(j.List(j.Nil(), j.Id("err"))),
		).Line(),
		j.Id("val").Op(":=").Qual(JSONTypes, "NewNormalizedValue").Call(j.Id("string").Call(j.Id("j"))),
		j.Return(j.List(j.Op("&").Id("val"), j.Nil())),
	).Line()

}

func (s *configDataSourceHandler) model(f *j.File, m *protogen.Message, l zerolog.Logger) {
	f.Type().Id(s.dataModelName(m)).Struct(
		s.modelFields(m.Fields, l)...,
	).Line()

}

// datasources require separate schema
func (s *configDataSourceHandler) schema(f *j.File, m *protogen.Message, structName string, resourceMetadata resourceMap, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Schema",
		j.Id("response").Dot("Schema").Op("=").Id(fmt.Sprintf("GenSchema%sDataSource", m.GoIdent.GoName)).Call(j.Id("ctx")),
	)
}

func (s *configDataSourceHandler) metadata(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Metadata",
		j.Id("response").Dot("TypeName").Op("=").Id("request").Dot("ProviderTypeName").Op("+").Lit(fmt.Sprintf("_%s", strcase.ToSnake(m.GoIdent.GoName))),
	)
}

func (s *configDataSourceHandler) configure(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Configure",
		j.If(
			j.Id("request").Dot("ProviderData").Op("==").Nil().Block(
				j.Return(),
			),
		),
		j.List(
			j.Id("client"), j.Id("ok"),
		).Op(":=").Id("request").Dot("ProviderData").Assert(
			j.Qual(AuthwiseManagementClient, "AuthwiseManagementServiceClient"),
		),
		j.If(j.Op("!").Id("ok").Block(
			j.Id("response").Dot("Diagnostics").Dot("AddError").Call(j.Lit("Unexpected Resource Configure Type"), j.Qual("fmt", "Sprintf").Call(
				j.Lit("Expected *v1alpha12.AuthwiseManagementServiceClient, got: %T. Please report this issue to the provider developers."),
				j.Id("request").Dot("ProviderData"),
			)),
			j.Return(),
		),
			j.Id("r").Dot("client").Op("=").Id("client"),
		),
	)
}

func (s *configDataSourceHandler) read(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {

	s.requestResponseMethod(f, structName, "Read",
		s.crudMethodTemplate(m,
			j.List(j.Id("res"), j.Id("err")).Op(":=").Id("r").Dot("computeResult").Call(j.List(j.Id("ctx"), j.Op("&").Id("data"))).Line(),
			j.If(j.Id("err").Op("!=").Nil()).Block(
				j.Return(),
			).Line(),
			j.Id("data").Dot("Result").Op("=").Op("*").Id("res"),
		)...,
	)
}

type ConfigDataSourceHandlerParams struct {
	SchemaHandler MessageHandler
	PackageName   string
}

func NewConfigDataSourceHandler(params ConfigDataSourceHandlerParams) MessageHandler {
	return &configDataSourceHandler{
		schemaHandler: params.SchemaHandler,
		packageImport: protogen.GoImportPath(params.PackageName),
	}
}
