package generate

import (
	j "github.com/dave/jennifer/jen"
)

func CrudMethodTemplate(name string, appendState bool, body ...j.Code) []j.Code {
	return []j.Code{
		j.Var().Id("data").Qual("gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1", name),
		j.Line(),
		j.Id("response").Dot("Diagnostics").Dot("Append").Call(
			j.Id("request").Dot("Plan").Dot("Get").Call(j.Id("ctx"), j.Op("&").Id("data")).Op("..."),
		),
		j.Line(),
		j.If(j.Id("response").Dot("Diagnostics").Dot("HasError").Call().Block(
			j.Return(),
		),
			j.Line(),
		),
		j.Add(body...),
	}
}
