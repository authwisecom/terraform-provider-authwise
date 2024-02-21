package converter

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	v1alpha12 "gitlab.authwise.io/authwise/api-client-go/authwise/types/core/v1alpha1"
	"terraform-provider-authwise/internal/model/authwise/types/core/v1alpha1"
)

func RealmToProto(r *v1alpha1.RealmModel) *v1alpha12.Realm {
	return &v1alpha12.Realm{
		Id:               r.Id.ValueString(),
		TenantId:         r.TenantId.ValueString(),
		UserDatabaseType: r.UserDatabaseType.ValueString(),
		Name:             r.Name.ValueString(),
		Description:      r.Description.ValueString(),
	}
}

func ProtoToRealm(p *v1alpha12.Realm, m *v1alpha1.RealmModel) {
	m.Id = types.StringValue(p.Id)
	m.Name = types.StringValue(p.Name)
	m.UserDatabaseType = types.StringValue(p.UserDatabaseType)
	m.Description = types.StringValue(p.Description)
	m.TenantId = types.StringValue(p.TenantId)
}
