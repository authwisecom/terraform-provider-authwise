package generate

import "google.golang.org/protobuf/compiler/protogen"

type MessageHandler interface {
	Handle(m *protogen.Message) error
}
