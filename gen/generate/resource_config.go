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

type configResourceHandler struct {
	plugin        *protogen.Plugin
	packageImport protogen.GoImportPath
	schemaHandler MessageHandler
}

func (s *configResourceHandler) Init(p *protogen.Plugin) {
	s.plugin = p
}

func (s *configResourceHandler) Finish(p *protogen.Plugin) {
}

func (s *configResourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "ConfigResource").Str("proto", m.GoIdent.GoName).Logger()
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

//only generating config data sources
//func (s *configResourceHandler) generateResource(m *protogen.Message, name string) error {
//
//	f := j.NewFile(PackageName)
//	gf := s.plugin.NewGeneratedFile(fmt.Sprintf("config_%s_resource.go", name), s.packageImport)
//	PrintGeneratedHeader(gf)
//	if _, err := gf.Write([]byte(f.GoString())); err != nil {
//		return err
//	}
//	return nil
//}

func (s *configResourceHandler) generateDataSource(m *protogen.Message, name string) error {
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

func (s *configResourceHandler) requestResponseMethod(f *j.File,
	structName, functionName string,
	statements ...j.Code) {

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id(functionName).Params(
		j.Id("ctx").Qual("context", "Context"),
		j.Id("request").Qual(DataSource, fmt.Sprintf("%sRequest", functionName)),
		j.Id("response").Op("*").Qual(DataSource, fmt.Sprintf("%sResponse", functionName)),
	).Block(statements...).Line()
}

func (s *configResourceHandler) crudMethodTemplate(m *protogen.Message, body ...j.Code) []j.Code {
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
		j.Qual("github.com/hashicorp/terraform-plugin-log/tflog", "Trace").Call(
			j.Id("ctx"),
			j.Lit(fmt.Sprintf("%s %s", strings.ToLower(name), "read")),
		).Line(),
		j.Id("response").Dot("Diagnostics").Dot("Append").Call(
			j.Id("response").Dot("State").Dot("Set").Call(j.Id("ctx"), j.Op("&").Id("data")).Op("..."),
		),
	}

	return result
}

func (s *configResourceHandler) datasource(f *j.File, m *protogen.Message, l zerolog.Logger) {

	name := m.GoIdent.GoName
	structName := strcase.ToLowerCamel(name) + "DataSource"

	f.Type().Id(structName).Struct(
		j.Id("client").Qual("gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1", "AuthwiseManagementServiceClient"),
	)

	f.Func().Id(fmt.Sprintf("New%s", name)).Params().Qual(DataSource, "DataSource").Block(
		j.Return(j.Op("&").Id(structName).Values()),
	).Line()

	s.dataModel(f, m, l)
	s.toName(f, m, structName, resourceMetadataMap, l)
	s.toProto(f, m, structName, l)
	s.toModel(f, m, structName, l)
	s.configure(f, m, structName, l)
	s.metadata(f, m, structName, l)
	s.schema(f, m, structName, resourceMetadataMap, l)
	s.read(f, m, structName, l)

}

func (s *configResourceHandler) dataModelName(m *protogen.Message) string {
	return strcase.ToLowerCamel(m.GoIdent.GoName) + "DataSourceModel"
}

func (s *configResourceHandler) modelFields(fields []*protogen.Field) []j.Code {
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

func (s *configResourceHandler) dataModel(f *j.File, m *protogen.Message, l zerolog.Logger) {

	f.Type().Id(s.dataModelName(m)).Struct(
		s.modelFields(m.Fields)...,
	).Line()

}

// datasources require separate schema
func (s *configResourceHandler) schema(f *j.File, m *protogen.Message, structName string, resourceMetadata resourceMap, l zerolog.Logger) {
	name := m.GoIdent.GoName
	metaMap, ok := resourceMetadata[name]
	if !ok {
		return
	}
	dsMeta := metaMap.datasourceMetadata

	//datasource schema is different from resource schema
	s.requestResponseMethod(f, structName, "Schema",
		j.Id("response").Dot("Schema").Op("=").Qual("github.com/hashicorp/terraform-plugin-framework/datasource/schema", "Schema").Values(
			j.Dict{
				j.Id("Attributes"): j.Map(j.String()).Qual("github.com/hashicorp/terraform-plugin-framework/datasource/schema", "Attribute").Values(
					j.DictFunc(func(d j.Dict) {
						for _, v := range dsMeta.schemaIdentifiers {
							l.Printf("test: %s", v)
							d[j.Lit(v)] = j.Qual("github.com/hashicorp/terraform-plugin-framework/datasource/schema", "StringAttribute").Values(
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

func (s *configResourceHandler) metadata(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Metadata",
		j.Id("response").Dot("TypeName").Op("=").Id("request").Dot("ProviderTypeName").Op("+").Lit(fmt.Sprintf("_%s", strcase.ToSnake(m.GoIdent.GoName))),
	)
}

func (s *configResourceHandler) configure(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Configure",
		j.If(
			j.Id("request").Dot("ProviderData").Op("==").Nil().Block(
				j.Return(),
			),
		),
		j.List(
			j.Id("client"), j.Id("ok"),
		).Op(":=").Id("request").Dot("ProviderData").Assert(
			j.Qual("gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1", "AuthwiseManagementServiceClient"),
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

func (s *configResourceHandler) toProto(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName
	dict := j.DictFunc(func(d j.Dict) {
		for _, fi := range m.Fields {
			valueFunc := "nil"
			switch fi.Desc.Kind() {
			case protoreflect.StringKind:
				valueFunc = "ValueString"
			case protoreflect.BoolKind:
				valueFunc = "ValueBool"
			default:
				valueFunc = "nil"
			}
			if valueFunc == "nil" {
				d[j.Id(fi.GoName)] = j.Nil()
			} else {
				d[j.Id(fi.GoName)] = j.Id("m").Dot(fi.GoName).Dot(valueFunc).Call()
			}
		}
	})

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("toProto").Params(
		j.Id("m").Op("*").Qual("", s.dataModelName(m)),
	).Op("*").Qual("gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1", name).Block(
		j.Return(
			j.Op("&").Qual("gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1", name).Values(
				dict,
			),
		),
	).Line()
}

// TODO - handle objects
func (s *configResourceHandler) toModel(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("toModel").Params(
		j.Id("p").Op("*").Qual("gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1", name),
		j.Id("m").Op("*").Id(s.dataModelName(m)),
	).BlockFunc(func(group *j.Group) {
		for _, fi := range m.Fields {
			path := "github.com/hashicorp/terraform-plugin-framework/types"
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

func (s *configResourceHandler) toName(f *j.File, m *protogen.Message, structName string, metadata resourceMap, l zerolog.Logger) {

	//val, ok := metadata[m.GoIdent.GoName]
	//if !ok {
	//	return
	//}
	//
	//var fmtParams []j.Code
	//for _, id := range val.nameFuncIdentifiers {
	//	fmtParams = append(fmtParams, j.Id("data").Dot(id).Dot("ValueString").Call())
	//}
	//
	//f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("toName").Params(
	//	j.Id("data").Id(s.dataModelName(m)),
	//).String().Block(
	//	j.Return(
	//		j.Qual("fmt", "Sprintf").Call(
	//			j.Lit(val.nameFuncPattern),
	//			j.List(
	//				fmtParams...,
	//			),
	//		),
	//	),
	//)
}

func (s *configResourceHandler) read(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	s.requestResponseMethod(f, structName, "Read",
		s.crudMethodTemplate(m,
			j.Id("req").Op(":=").Op("&").Qual(
				"gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1",
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

type ConfigResourceHandlerParams struct {
	SchemaHandler MessageHandler
	PackageName   string
}

func NewConfigResourceHandler(params ConfigResourceHandlerParams) MessageHandler {
	return &configResourceHandler{
		schemaHandler: params.SchemaHandler,
		packageImport: protogen.GoImportPath(params.PackageName),
	}
}
