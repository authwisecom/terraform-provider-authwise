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

type standardDataSourceHandler struct {
	plugin        *protogen.Plugin
	packageImport protogen.GoImportPath
	schemaHandler MessageHandler
}

func (s *standardDataSourceHandler) Init(p *protogen.Plugin) {
	s.plugin = p
}

func (s *standardDataSourceHandler) Finish(p *protogen.Plugin) {
}

func (s *standardDataSourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "StandardDataSource").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating")
	err := s.schemaHandler.Handle(m)

	f := j.NewFile(PackageName)
	s.datasource(f, m, l)

	gf := s.plugin.NewGeneratedFile(fmt.Sprintf("%s_data_source.go", strcase.ToSnake(string(m.Desc.Name()))), s.packageImport)
	PrintGeneratedHeader(gf)
	gf.P(f.GoString())

	if err != nil {
		return err
	}
	l.Debug().Msg("Finished generating")
	return nil
}

func (s *standardDataSourceHandler) datasource(f *j.File, m *protogen.Message, l zerolog.Logger) {

	name := m.GoIdent.GoName
	structName := strcase.ToLowerCamel(name) + "DataSource"

	f.Type().Id(structName).Struct(
		j.Id("client").Qual(AuthwiseManagementClient, "AuthwiseManagementServiceClient"),
	)

	f.Func().Id(fmt.Sprintf("New%sDataSource", name)).Params().Qual(DataSource, "DataSource").Block(
		j.Return(j.Op("&").Id(structName).Values()),
	).Line()

	s.toName(f, m, structName, resourceMetadataMap, l)
	s.toModel(f, m, structName, l)
	s.dataModel(f, m, l)
	s.configure(f, m, structName, l)
	s.metadata(f, m, structName, l)
	s.schema(f, m, structName, resourceMetadataMap, l)
	s.read(f, m, structName, l)

}

func (s *standardDataSourceHandler) dataModelName(m *protogen.Message) string {
	return strcase.ToLowerCamel(m.GoIdent.GoName) + "DataSourceModel"
}

func (s *standardDataSourceHandler) modelFields(fields []*protogen.Field) []j.Code {
	var result []j.Code
	for _, f := range fields {
		typeName := "Object"
		schemaName := strcase.ToSnake(f.GoName)
		// TODO - Build out this set of data
		switch f.Desc.Kind() {
		case protoreflect.StringKind:
			typeName = "String"
		case protoreflect.BoolKind:
			typeName = "Bool"
		}
		log.Debug().Str("kind", f.Desc.Kind().GoString()).Msg("processing model field")
		result = append(result, j.Id(f.GoName).Qual(Types, typeName).Tag(map[string]string{
			"tfsdk": schemaName,
		}))
	}
	return result
}

func (s *standardDataSourceHandler) modelName(m *protogen.Message) string {
	return strcase.ToLowerCamel(m.GoIdent.GoName) + "DataSourceModel"
}

func (s *standardDataSourceHandler) toModel(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("toModel").Params(
		j.Id("p").Op("*").Qual(TypesCore, name),
		j.Id("m").Op("*").Id(s.modelName(m)),
	).BlockFunc(func(group *j.Group) {
		for _, fi := range m.Fields {
			path := Types
			typesFunc := "nil"

			switch fi.Desc.Kind() {
			case protoreflect.StringKind:
				typesFunc = "StringValue"
			case protoreflect.BoolKind:
				typesFunc = "BoolValue"
			default:
				typesFunc = "nil"
			}

			if typesFunc != "nil" {
				group.Id("m").Dot(fi.GoName).Op("=").Qual(path, typesFunc).Call(j.Id("p").Dot(fi.GoName))
			}
		}
	}).Line()
}

func (s *standardDataSourceHandler) requestResponseMethod(f *j.File,
	structName, functionName string,
	statements ...j.Code) {

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id(functionName).Params(
		j.Id("ctx").Qual("context", "Context"),
		j.Id("request").Qual(DataSource, fmt.Sprintf("%sRequest", functionName)),
		j.Id("response").Op("*").Qual(DataSource, fmt.Sprintf("%sResponse", functionName)),
	).Block(statements...).Line()
}

func (s *standardDataSourceHandler) crudMethodTemplate(m *protogen.Message, body ...j.Code) []j.Code {
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
		j.Id("r").Dot("toModel").Call(j.Id("resp"), j.Op("&").Id("data")).Line(),
		j.Qual(TFLog, "Trace").Call(
			j.Id("ctx"),
			j.Lit(fmt.Sprintf("%s %s", strings.ToLower(name), "read")),
		).Line(),
		j.Id("response").Dot("Diagnostics").Dot("Append").Call(
			j.Id("response").Dot("State").Dot("Set").Call(j.Id("ctx"), j.Op("&").Id("data")).Op("..."),
		),
	}

	return result
}

func (s *standardDataSourceHandler) dataModel(f *j.File, m *protogen.Message, l zerolog.Logger) {

	f.Type().Id(s.dataModelName(m)).Struct(
		s.modelFields(m.Fields)...,
	).Line()

}

func (s *standardDataSourceHandler) toName(f *j.File, m *protogen.Message, structName string, metadata resourceMap, l zerolog.Logger) {

	val, ok := metadata[m.GoIdent.GoName]
	if !ok {
		return
	}

	var fmtParams []j.Code
	for _, id := range val.resourceMetadata.nameFuncIdentifiers {
		fmtParams = append(fmtParams, j.Id("data").Dot(id).Dot("ValueString").Call())
	}

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("toName").Params(
		j.Id("data").Id(s.modelName(m)),
	).String().Block(
		j.Return(
			j.Qual("fmt", "Sprintf").Call(
				j.Lit(val.nameFuncPattern),
				j.List(
					fmtParams...,
				),
			),
		),
	)
}

func (s *standardDataSourceHandler) configure(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
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

func (s *standardDataSourceHandler) metadata(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Metadata",
		j.Id("response").Dot("TypeName").Op("=").Id("request").Dot("ProviderTypeName").Op("+").Lit(fmt.Sprintf("_%s", strcase.ToSnake(m.GoIdent.GoName))),
	)
}

func (s *standardDataSourceHandler) schema(f *j.File, m *protogen.Message, structName string, resourceMetadata resourceMap, l zerolog.Logger) {
	name := m.GoIdent.GoName
	metaMap, ok := resourceMetadata[name]
	if !ok {
		return
	}
	dsMeta := metaMap.datasourceMetadata

	//datasource schema is different from resource schema
	s.requestResponseMethod(f, structName, "Schema",
		j.Id("response").Dot("Schema").Op("=").Qual(DataSourceSchema, "Schema").Values(
			j.Dict{
				j.Id("Attributes"): j.Map(j.String()).Qual(DataSourceSchema, "Attribute").Values(
					j.DictFunc(func(d j.Dict) {
						for _, v := range dsMeta.schemaIdentifiers {
							l.Printf("test: %s", v)
							d[j.Lit(v)] = j.Qual(DataSourceSchema, "StringAttribute").Values(
								j.Dict{
									j.Id("Description"): j.Lit(""),
									j.Id("Required"):    j.True(),
								},
							)
						}
					}),
				),
			},
		),
	)
}

func (s *standardDataSourceHandler) read(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	s.requestResponseMethod(f, structName, "Read",
		s.crudMethodTemplate(m,
			j.Id("req").Op(":=").Op("&").Qual(
				AuthwiseManagementClient,
				fmt.Sprintf("Get%sRequest", name),
			).Values(
				j.Dict{
					j.Id("Name"): j.Id("r").Dot("toName").Call(
						j.Id("data"),
					),
				},
			).Line(),
			j.List(j.Id("resp"), j.Id("err")).Op(":=").Id("r").Dot("client").Dot(
				fmt.Sprintf("Get%s", name),
			).Call(j.Id("ctx"), j.Id("req")).Line(),
			j.If(j.Id("err").Op("!=").Nil()).Block(
				j.Id("response").Dot("Diagnostics").Dot("AddError").Call(
					j.Lit(fmt.Sprintf("Error Reading %s", name)),
					j.Id("err").Dot("Error").Call(),
				),
				j.Return(),
			).Line(),
		)...,
	)
}

type StandardDataSourceHandlerParams struct {
	SchemaHandler MessageHandler
	PackageName   string
}

func NewStandardDataSourceHandler(params StandardDataSourceHandlerParams) MessageHandler {
	return &standardDataSourceHandler{
		schemaHandler: params.SchemaHandler,
		packageImport: protogen.GoImportPath(params.PackageName),
	}
}
