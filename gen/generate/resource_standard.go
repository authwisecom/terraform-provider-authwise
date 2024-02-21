package generate

import (
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
	if err != nil {
		return err
	}
	l.Debug().Msg("Finished generating")
	return nil
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
