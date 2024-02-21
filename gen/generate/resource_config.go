package generate

import (
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/compiler/protogen"
)

type configResourceHandler struct {
	schemaHandler MessageHandler
}

func (s *configResourceHandler) Handle(m *protogen.Message) error {
	l := log.With().Str("generator", "ConfigResource").Str("proto", m.GoIdent.GoName).Logger()
	l.Debug().Msg("Generating")
	l.Debug().Msg("Finished generating")
	return nil
}

type ConfigResourceHandlerParams struct {
	SchemaHandler MessageHandler
}

func NewConfigResourceHandler(params ConfigResourceHandlerParams) MessageHandler {
	return &configResourceHandler{schemaHandler: params.SchemaHandler}
}
