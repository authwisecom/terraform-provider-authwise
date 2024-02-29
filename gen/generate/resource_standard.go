package generate

import (
	"fmt"
	j "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
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
		j.Id("client").Qual("gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1", "AuthwiseManagementServiceClient"),
	)

	f.Func().Id(fmt.Sprintf("New%s", name)).Params().Qual(Resource, "Resource").Block(
		j.Return(j.Op("&").Id(structName).Values()),
	).Line()

	s.model(f, m, l)
	s.configure(f, m, structName)
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

func (s *standardResourceHandler) requestResponseMethod(f *j.File,
	structName, functionName string,
	statements ...j.Code) {

	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id(functionName).Params(
		j.Id("ctx").Qual("context", "Context"),
		j.Id("request").Qual(Resource, fmt.Sprintf("%sRequest", functionName)),
		j.Id("response").Op("*").Qual(Resource, fmt.Sprintf("%sResponse", functionName)),
	).Block(statements...).Line()
}

func (s *standardResourceHandler) toProto(f *j.File, m *protogen.Message, structName string) {
	name := m.GoIdent.GoName
	f.Func().Params(j.Id("r").Op("*").Id(structName)).Id("toProto").Params(
		j.Id("m").Op("*").Qual("gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1", name),
	).Qual("", "").Block()
}

func (s *standardResourceHandler) configure(f *j.File, m *protogen.Message, structName string) {
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

// TODO - request objects can have arbitrary fields
func (s *standardResourceHandler) create(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	name := m.GoIdent.GoName

	s.requestResponseMethod(f, structName, "Create",
		CrudMethodTemplate(name, true,
			j.Id("cr").Op(":=").Op("&").Qual(
				"gitlab.authwise.io/authwise/api-client-go/authwise/management/v1alpha1",
				fmt.Sprintf("Create%sRequest", name),
			).Values(),
		)...,
	)
}

func (s *standardResourceHandler) read(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Read")
}

func (s *standardResourceHandler) update(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Update")
}

func (s *standardResourceHandler) delete(f *j.File, m *protogen.Message, structName string, l zerolog.Logger) {
	s.requestResponseMethod(f, structName, "Delete")
}

/*
func (a *appearanceProfile) Metadata(ctx context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	//TODO implement me
	panic("implement me")
}
*/

/*
func (a *appearanceProfile) Schema(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
	//TODO implement me
	panic("implement me")
}
*/

/*
func (a *appearanceProfile) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	//TODO implement me
	panic("implement me")
}
*/

/*
func (a *appearanceProfile) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	//TODO implement me
	panic("implement me")
}
*/

/*
func (a *appearanceProfile) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	//TODO implement me
	panic("implement me")
}
*/

/*
func (a *appearanceProfile) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	//TODO implement me
	panic("implement me")
}
*/

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
