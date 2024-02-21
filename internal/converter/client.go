package converter

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	v1alpha12 "gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1"
	"terraform-provider-authwise/internal/model/authwise/types/core/v1alpha1"
)

func ClientToProto(r *v1alpha1.ClientModel) *v1alpha12.Client {
	return &v1alpha12.Client{
		Id:                  r.Id.ValueString(),
		AudienceId:          r.AudienceId.ValueString(),
		AppearanceProfileId: r.AppearanceProfileId.ValueString(),
		Name:                r.Name.ValueString(),
		Alias:               r.Alias.ValueString(),
		GrantType:           r.GrantType.ValueString(),
		LoginUrl:            r.LoginUrl.ValueString(),
		LogoId:              r.LogoId.ValueString(),
		Config:              nil,
		Metadata:            nil,
	}
}

func ProtoToClient(p *v1alpha12.Client, m *v1alpha1.ClientModel) {
	m.Id = types.StringValue(p.Id)
	m.Name = types.StringValue(p.Name)
	m.Alias = types.StringValue(p.Alias)
	m.LoginUrl = types.StringValue(p.LoginUrl)
	m.GrantType = types.StringValue(p.GrantType)
	m.LogoId = types.StringValue(p.LogoId)
	m.AudienceId = types.StringValue(p.AudienceId)
	m.AppearanceProfileId = types.StringValue(p.AppearanceProfileId)
}
