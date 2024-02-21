package types

// needs
// Model, Schema, Converters
// Generate the converters for now?
type ResourceInput struct {
	Name                string
	NameLower           string
	NamePrefix          string
	NameField           string
	OutputFileName      string
	Package             string
	CreateRequestParams map[string]interface{}
}

type TemplateResourceInput struct {
	TemplateContent string
	OutputPath      string
	Package         string
	Resources       []*ResourceInput
}
