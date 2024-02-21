package generate

import (
	"fmt"
	j "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/compiler/protogen"
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

	if err = s.generateResource(m, snakeName); err != nil {
		return err
	}
	if err = s.generateDataSource(m, snakeName); err != nil {
		return err
	}

	l.Debug().Msg("Finished generating")
	return nil
}

func (s *configResourceHandler) generateResource(m *protogen.Message, name string) error {

	f := j.NewFile(PackageName)
	gf := s.plugin.NewGeneratedFile(fmt.Sprintf("config_%s_resource.go", name), s.packageImport)
	PrintGeneratedHeader(gf)
	if _, err := gf.Write([]byte(f.GoString())); err != nil {
		return err
	}
	return nil
}

func (s *configResourceHandler) generateDataSource(m *protogen.Message, name string) error {

	f := j.NewFile(PackageName)
	gf := s.plugin.NewGeneratedFile(fmt.Sprintf("config_%s_data_source.go", name), s.packageImport)
	PrintGeneratedHeader(gf)
	if _, err := gf.Write([]byte(f.GoString())); err != nil {
		return err
	}
	return nil
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
