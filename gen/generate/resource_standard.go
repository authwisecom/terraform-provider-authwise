package generate

import (
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/compiler/protogen"
)

type standardResourceHandler struct {
	schemaHandler MessageHandler
}

func (s *standardResourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "StandardResource").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating")
	l.Debug().Msg("Finished generating")
	return nil
}

type StandardResourceHandlerParams struct {
	SchemaHandler MessageHandler
}

func NewStandardResourceHandler(params StandardResourceHandlerParams) MessageHandler {

	return &standardResourceHandler{schemaHandler: params.SchemaHandler}
}
