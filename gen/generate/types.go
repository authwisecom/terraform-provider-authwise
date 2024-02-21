package generate

import "google.golang.org/protobuf/compiler/protogen"

const (
	PackageName = "provider"
	ProgramName = "terraform-provider-authwise"
)

type MessageHandler interface {
	Init(p *protogen.Plugin)
	Handle(m *protogen.Message) error
	Finish(p *protogen.Plugin)
}
