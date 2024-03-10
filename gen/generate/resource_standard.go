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

type standardResourceHandler struct {
	plugin        *protogen.Plugin
	packageImport protogen.GoImportPath
	schemaHandler MessageHandler
}

func (s *standardResourceHandler) Init(p *protogen.Plugin) {
	s.plugin = p
}

func (s *standardResourceHandler) Finish(p *protogen.Plugin) {
}

func (s *standardResourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "StandardResource").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating")
	err := s.schemaHandler.Handle(m)

	f := j.NewFile(PackageName)
	s.resource(f, m, l)

	gf := s.plugin.NewGeneratedFile(fmt.Sprintf("%s_resource.go", strcase.ToSnake(string(m.Desc.Name()))), s.packageImport)
	PrintGeneratedHeader(gf)
	gf.P(f.GoString())

	if err != nil {
		return err
	}
	l.Debug().Msg("Finished generating")
	return nil
}

func (s *standardResourceHandler) resource(f *j.File, m *protogen.Message, l zerolog.Logger) {

	name := m.GoIdent.GoName
	structName := strcase.ToLowerCamel(name) + "Resource"

	f.Type().Id(structName).Struct(
		j.Id("client").Qual(AuthwiseManagementClient, "AuthwiseManagementServiceClient"),
	)

	f.Func().Id(fmt.Sprintf("New%s", name)).Params().Qual(Resource, "Resource").Block(
		j.Return(j.Op("&").Id(structName).Values()),
	).Line()

	s.model(f, m, l)
	s.toName(f, m, structName, resourceMetadataMap, l)
	s.toProto(f, m, structName, l)
	s.toModel(f, m, structName, l)
	s.configure(f, m, structName, l)
	s.metadata(f, m, structName, l)
	s.schema(f, m, structName, l)
	s.create(f, m, structName, l)
	s.read(f, m, structName, l)
	s.update(f, m, structName, l)
	s.delete(f, m, structName, l)

}

func (s *standardResourceHandler) modelName(m *protogen.Message) string {
	return strcase.ToLowerCamel(m.GoIdent.GoName) + "Model"
}

func (s *standardResourceHandler) model(f *j.File, m *protogen.Message, l zerolog.Logger) {

	f.Type().Id(s.modelName(m)).Struct(
		s.modelFields(m.Fields)...,
	).Line()

}

func (s *standardResourceHandler) modelFields(fields []*protogen.Field) []j.Code {
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

func (s *standardResourceHandler) crudMethodTemplate(m *protogen.Message, operation string, fromState bool, appendState bool, body ...j.Code) []j.Code {
	name := m.GoIdent.GoName
	requestObjName := "Plan"
	if fromState {
		requestObjName = "State"
	}
	//standard crud method start
	result := []j.Code{
		j.Var().Id("data").Qual("", s.modelName(m)).Line(),
		j.Id("response").Dot("Diagnostics").Dot("Append").Call(
			j.Id("request").Dot(requestObjName).Dot("Get").Call(j.Id("ctx"), j.Op("&").Id("data")).Op("..."),
		).Line(),
		j.If(j.Id("response").Dot("Diagnostics").Dot("HasError").Call().Block(
			j.Return(),
		).Line()),
		j.Add(body...),
	}

	//append state to tf
	if appendState {
		result = append(result, j.Add(
			j.Id("r").Dot("toModel").Call(j.Id("resp"), j.Op("&").Id("data")).Line(),
			j.Qual(TFLog, "Trace").Call(
				j.Id("ctx"),
				j.Lit(fmt.Sprintf("%s %s", strings.ToLower(name), operation)),
			).Line(),
			j.Id("response").Dot("Diagnostics").Dot("Append").Call(
				j.Id("response").Dot("State").Dot("Set").Call(j.Id("ctx"), j.Op("&").Id("data")).Op("..."),
			),
		))
	}

	return result
}

func (s *standardResourceHandler) requestResponseMethod(f *j.File,
	structName, functionName string,
	statements ...j.Code) {

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id(functionName).Params(
		j.Id("ctx").Qual("context", "Context"),
		j.Id("request").Qual(Resource, fmt.Sprintf("%sRequest", functionName)),
		j.Id("response").Op("*").Qual(Resource, fmt.Sprintf("%sResponse", functionName)),
	).Block(statements...).Line()
}

func (s *standardResourceHandler) toName(f *j.File, m *protogen.Message, structName string, metadata resourceMap, l zerolog.Logger) {

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

func (s *standardResourceHandler) objectToProtoStruct(objMap *map[string]interface{}, protoType string) {

}

// TODO - handle objects
func (s *standardResourceHandler) toProto(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName
	//type field fi.Desc.FullName()

	errCheck := j.If(j.Id("err").Op("!=").Nil().Block(j.Return(j.Nil(), j.Id("err"))))

	var conversions []j.Code
	dict := j.DictFunc(func(d j.Dict) {
		for _, fi := range m.Fields {
			valueFunc := ""
			switch fi.Desc.Kind() {
			case protoreflect.StringKind:
				valueFunc = "ValueString"
			case protoreflect.BoolKind:
				valueFunc = "ValueBool"
			case protoreflect.Int64Kind:
				valueFunc = "ValueInt64"
			case protoreflect.MessageKind:
				//objects
				switch fi.Desc.Message().FullName().Name() {
				case "Any":
					code := j.List(
						j.Id(ConversionName(strings.ToLower(fi.GoName))),
						j.Id("err")).Op(":=").Qual(Util, "ObjectToProtoAny").Call(j.Id("m").Dot(fi.GoName)).Line().Add(errCheck)
					conversions = append(conversions, code)
				case "Struct":
					code := j.List(
						j.Id(ConversionName(strings.ToLower(fi.GoName))),
						j.Id("err")).Op(":=").Qual(Util, "ObjectToProtoStruct").Call(j.Id("m").Dot(fi.GoName)).Line().Add(errCheck)
					conversions = append(conversions, code)
				}
			}

			if valueFunc == "" {
				//objects requiring conversion
				d[j.Id(fi.GoName)] = j.Id(ConversionName(strings.ToLower(fi.GoName)))
			} else {
				//standard
				d[j.Id(fi.GoName)] = j.Id("m").Dot(fi.GoName).Dot(valueFunc).Call()
			}
		}
	})

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("toProto").Params(
		j.Id("m").Op("*").Qual("", s.modelName(m)),
	).Parens(j.List(j.Op("*").Qual(TypesCore, name), j.Id("error"))).Block(
		append(conversions, j.Return(
			j.Op("&").Qual(TypesCore, name).Values(
				dict,
			),
			j.Nil(),
		))...,
	).Line()
}

// TODO - handle objects
func (s *standardResourceHandler) toModel(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
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

func (s *standardResourceHandler) configure(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
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

func (s *standardResourceHandler) metadata(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Metadata",
		j.Id("response").Dot("TypeName").Op("=").Id("request").Dot("ProviderTypeName").Op("+").Lit(fmt.Sprintf("_%s", strcase.ToSnake(m.GoIdent.GoName))),
	)
}

func (s *standardResourceHandler) schema(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Schema",
		j.Id("response").Dot("Schema").Op("=").Id(fmt.Sprintf("GenSchema%s", m.GoIdent.GoName)).Call(j.Id("ctx")),
	)
}

// TODO - make this common?
func (s *standardResourceHandler) protoConversion(name string) []j.Code {
	return []j.Code{
		j.List(j.Id("val"), j.Id("err")).Op(":=").Id("r").Dot("toProto").Call(j.Op("&").Id("data")).Line(),
		j.If(
			j.Id("err").Op("!=").Nil().Block(
				j.Id("response").Dot("Diagnostics").Dot("AddError").Call(
					j.Lit(fmt.Sprintf("Error Converting %s to Proto", name)),
					j.Id("err").Dot("Error").Call(),
				),
				j.Return(),
			),
		).Line(),
	}
}

// TODO - request objects can have arbitrary fields
func (s *standardResourceHandler) create(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	s.requestResponseMethod(f, structName, "Create",
		s.crudMethodTemplate(m, "create", false, true,
			j.Add(s.protoConversion(name)...).Line(),
			j.Id("req").Op(":=").Op("&").Qual(
				AuthwiseManagementClient,
				fmt.Sprintf("Create%sRequest", name),
			).Values(
				j.Dict{
					j.Id(name): j.Id("val"),
				}).Line(),
			j.List(j.Id("resp"), j.Id("err")).Op(":=").Id("r").Dot("client").Dot(
				fmt.Sprintf("Create%s", name),
			).Call(j.Id("ctx"), j.Id("req")).Line(),
			j.If(j.Id("err").Op("!=").Nil()).Block(
				j.Id("response").Dot("Diagnostics").Dot("AddError").Call(
					j.Lit(fmt.Sprintf("Error Creating %s", name)),
					j.Id("err").Dot("Error").Call(),
				),
				j.Return(),
			).Line(),
		)...,
	)
}

func (s *standardResourceHandler) read(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	s.requestResponseMethod(f, structName, "Read",
		s.crudMethodTemplate(m, "read", true, true,
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

func (s *standardResourceHandler) update(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	s.requestResponseMethod(f, structName, "Update",
		s.crudMethodTemplate(m, "update", false, true,
			j.Add(s.protoConversion(name)...).Line(),
			j.Id("req").Op(":=").Op("&").Qual(
				AuthwiseManagementClient,
				fmt.Sprintf("Update%sRequest", name),
			).Values(
				j.Dict{
					j.Id("Name"): j.Id("r").Dot("toName").Call(
						j.Id("data"),
					),
					j.Id(name): j.Id("val"),
				},
			).Line(),
			j.List(j.Id("resp"), j.Id("err")).Op(":=").Id("r").Dot("client").Dot(
				fmt.Sprintf("Update%s", name),
			).Call(j.Id("ctx"), j.Id("req")).Line(),
			j.If(j.Id("err").Op("!=").Nil()).Block(
				j.Id("response").Dot("Diagnostics").Dot("AddError").Call(
					j.Lit(fmt.Sprintf("Error Updating %s", name)),
					j.Id("err").Dot("Error").Call(),
				),
				j.Return(),
			).Line(),
		)...,
	)
}

func (s *standardResourceHandler) delete(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName
	s.requestResponseMethod(f, structName, "Delete",
		s.crudMethodTemplate(m, "delete", true, false,
			j.Id("req").Op(":=").Op("&").Qual(
				AuthwiseManagementClient,
				fmt.Sprintf("Delete%sRequest", name),
			).Values(
				j.Dict{
					j.Id("Name"): j.Id("r").Dot("toName").Call(
						j.Id("data"),
					),
				},
			).Line(),
			j.List(j.Id("_"), j.Id("err")).Op(":=").Id("r").Dot("client").Dot(
				fmt.Sprintf("Delete%s", name),
			).Call(j.Id("ctx"), j.Id("req")).Line(),
			j.If(j.Id("err").Op("!=").Nil()).Block(
				j.Id("response").Dot("Diagnostics").Dot("AddError").Call(
					j.Lit(fmt.Sprintf("Error Deleting %s", name)),
					j.Id("err").Dot("Error").Call(),
				),
				j.Return(),
			).Line(),
			j.Return(),
		)...,
	)
}

type StandardResourceHandlerParams struct {
	SchemaHandler MessageHandler
	PackageName   string
}

func NewStandardResourceHandler(params StandardResourceHandlerParams) MessageHandler {
	return &standardResourceHandler{
		schemaHandler: params.SchemaHandler,
		packageImport: protogen.GoImportPath(params.PackageName),
	}
}
