package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// attributeGetter is a plan or a state.
type attributeGetter interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}

// keepStrings carries a string attribute's planned or prior value across a
// write or read that comes back otherwise. It is for create-time inputs
// kit reads back as something else afterwards:
//
//   - `path = ""`, the documented root (tf#57, kit#911), which the
//     generated runtime reads back as null;
//   - an issuer's deprecated domain_id and path (kit#911), which kit reads
//     from the issuer's first binding, and so moves when that binding goes.
//
// An unknown or null value in from is not kept: the server's stands.
func keepStrings(ctx context.Context, from attributeGetter, to *tfsdk.State, diags *diag.Diagnostics, attrs ...string) {

	if from == nil || to == nil || to.Raw.IsNull() {
		return
	}
	for _, a := range attrs {
		var was, now types.String
		if from.GetAttribute(ctx, path.Root(a), &was).HasError() || to.GetAttribute(ctx, path.Root(a), &now).HasError() {
			continue
		}
		if was.IsNull() || was.IsUnknown() || was.Equal(now) {
			continue
		}
		diags.Append(to.SetAttribute(ctx, path.Root(a), was)...)
	}
}
