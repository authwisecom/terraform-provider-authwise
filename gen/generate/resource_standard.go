package generate

import (
	"fmt"
	j "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/compiler/protogen"
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
	s.plugin = p
}

func (s *standardResourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "StandardResource").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating")
	err := s.schemaHandler.Handle(m)

	f := j.NewFile(PackageName)
	s.resource(f, m)

	gf := s.plugin.NewGeneratedFile(fmt.Sprintf("%s_resource.go", strcase.ToSnake(string(m.Desc.Name()))), s.packageImport)
	gf.P(f.GoString())

	if err != nil {
		return err
	}
	l.Debug().Msg("Finished generating")
	return nil
}

func (s *standardResourceHandler) resource(f *j.File, m *protogen.Message) {

	name := m.GoIdent.GoName
	structName := strcase.ToLowerCamel(name) + "Resource"

	f.Type().Id(structName).Struct()

	f.Func().Id(fmt.Sprintf("New%s", name)).Params().Qual(Resource, "Resource").Block(
		j.Return(j.Op("&").Id(structName).Values()),
	).Line()

	s.metadata(f, m, structName)
	s.schema(f, m, structName)
	s.create(f, m, structName)
	s.read(f, m, structName)
	s.update(f, m, structName)
	s.delete(f, m, structName)

}

func (s *standardResourceHandler) requestResponseMethod(f *j.File,
	structName, functionName string,
	statements ...j.Code) {

	f.Func().Params(j.Op("*").Id(structName)).Id(functionName).Params(
		j.Id("ctx").Qual("context", "Context"),
		j.Id("request").Qual(Resource, fmt.Sprintf("%sRequest", functionName)),
		j.Id("response").Op("*").Qual(Resource, fmt.Sprintf("%sResponse", functionName)),
	).Block(statements...).Line()
}

func (s *standardResourceHandler) metadata(f *j.File, m *protogen.Message, structName string) {
	s.requestResponseMethod(f, structName, "Metadata")
}

func (s *standardResourceHandler) schema(f *j.File, m *protogen.Message, structName string) {
	s.requestResponseMethod(f, structName, "Schema",
		j.Id("response").Dot("Schema").Op("=").Id(fmt.Sprintf("GenSchema%s", m.GoIdent.GoName)).Call(j.Id("ctx")),
	)
}

func (s *standardResourceHandler) create(f *j.File, m *protogen.Message, structName string) {
	s.requestResponseMethod(f, structName, "Create")
}

func (s *standardResourceHandler) read(f *j.File, m *protogen.Message, structName string) {
	s.requestResponseMethod(f, structName, "Read")
}

func (s *standardResourceHandler) update(f *j.File, m *protogen.Message, structName string) {
	s.requestResponseMethod(f, structName, "Update")
}

func (s *standardResourceHandler) delete(f *j.File, m *protogen.Message, structName string) {
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
