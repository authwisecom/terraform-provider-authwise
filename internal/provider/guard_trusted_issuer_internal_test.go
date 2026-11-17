package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestKeysMissing(t *testing.T) {

	none := &guardTrustedIssuerModel{Discover: types.BoolNull(), JWKS: types.StringNull(), KeySourceNode: types.StringNull()}
	source := &guardTrustedIssuerModel{Discover: types.BoolNull(), JWKS: types.StringNull(), KeySourceNode: types.StringValue("n")}
	discover := &guardTrustedIssuerModel{Discover: types.BoolValue(true), JWKS: types.StringNull(), KeySourceNode: types.StringNull()}
	jwks := &guardTrustedIssuerModel{Discover: types.BoolNull(), JWKS: types.StringValue("{}"), KeySourceNode: types.StringNull()}
	unknown := &guardTrustedIssuerModel{Discover: types.BoolNull(), JWKS: types.StringUnknown(), KeySourceNode: types.StringNull()}

	assert.True(t, keysMissing(none, nil), "a new issuer needs its keys")
	assert.False(t, keysMissing(source, nil))
	assert.False(t, keysMissing(unknown, nil), "an unknown value may yet give them")
	assert.False(t, keysMissing(none, source), "removing a key source keeps the keys last reported")
	assert.False(t, keysMissing(none, none), "an issuer without a source stays as it is")
	assert.True(t, keysMissing(none, discover))
	assert.True(t, keysMissing(none, jwks))
}

func TestReplacesKeepingName(t *testing.T) {

	base := func() *guardTrustedIssuerModel {
		return &guardTrustedIssuerModel{
			TenantID: types.StringValue("t-1"), NetworkID: types.StringValue("n-1"), DisplayName: types.StringValue("ops"),
			Issuer: types.StringValue("https://a"), Discover: types.BoolNull(), JWKS: types.StringNull(), KeySourceNode: types.StringNull(),
		}
	}

	state, plan := base(), base()
	assert.False(t, replacesKeepingName(plan, state), "no replacement")

	plan.Issuer = types.StringValue("https://b")
	assert.True(t, replacesKeepingName(plan, state))
	plan.DisplayName = types.StringValue("ops-2")
	assert.False(t, replacesKeepingName(plan, state), "a new name is free")

	state, plan = base(), base()
	state.Discover = types.BoolValue(true)
	plan.KeySourceNode = types.StringValue("n")
	assert.True(t, replacesKeepingName(plan, state), "discover to a key source replaces")
}

func TestNameTakenHint(t *testing.T) {
	taken := status.Error(codes.AlreadyExists, "displayName: taken")
	assert.Contains(t, nameTakenHint(taken, taken.Error()), "give it a new display_name")
	other := status.Error(codes.InvalidArgument, "bad")
	assert.Equal(t, other.Error(), nameTakenHint(other, other.Error()))
}
