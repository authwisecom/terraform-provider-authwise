package provider

import (
	"context"
	"testing"
	"time"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestProtoattr_RoundTrip converts policies to Terraform values and back.
// protoattr is internal, so this is the one test in the package's own
// namespace.
func TestProtoattr_RoundTrip(t *testing.T) {

	type s struct {
		arrange func() *corepb.AuthenticationPolicy
	}

	requirement := &corepb.Requirement{
		Mode:                corepb.Requirement_TYPES,
		MinFactors:          2,
		AllowedFactorTypes:  []string{"totp", "webauthn"},
		RequiredFactorTypes: []string{"webauthn"},
		ReauthAfter:         durationpb.New(90 * time.Second),
		SkipIfDeviceTrusted: true,
		DenyReason:          "audit only",
		HardwareBound:       true,
	}

	cases := map[string]s{
		"every field kind": {
			arrange: func() *corepb.AuthenticationPolicy {
				return &corepb.AuthenticationPolicy{
					Rules: []*corepb.AuthenticationRule{
						{Name: "strict", Condition: "risk.level == 'high'", Require: requirement},
						{Name: "default"},
					},
					Floor: &corepb.Requirement{Mode: corepb.Requirement_ANY_FACTOR},
					Enrollment: &corepb.EnrollmentPolicy{
						InFlow:             true,
						Grace:              durationpb.New(72 * time.Hour),
						SelfServiceTypes:   []string{"totp"},
						OfferRecoveryCodes: true,
					},
					RememberDevice: &corepb.RememberDevicePolicy{Enabled: true, Ttl: durationpb.New(720 * time.Hour), MaxDevices: 5},
					Session:        &corepb.SessionPolicy{Absolute: durationpb.New(8 * time.Hour), FactorReauth: durationpb.New(1500 * time.Millisecond)},
					Throttle: &corepb.FactorThrottle{
						OtpAttempts: 5, TotpAttemptsPerWindow: 3, TotpWindow: durationpb.New(time.Minute),
						SendsPerHour: 10, RecoveryAttemptsPerHour: 4, PushesPerInteraction: 2,
					},
					Risk: &corepb.RiskPolicy{
						Mode:                 corepb.RiskPolicy_BOTH,
						ExternalEndpointName: "tenants/t-1/endpoints/e-1",
						Timeout:              durationpb.New(500 * time.Millisecond),
						FailMode:             corepb.RiskPolicy_OPEN,
						Weights:              map[string]int32{"new_device": 30, "impossible_travel": 80},
						MediumAt:             40,
						HighAt:               70,
						WorkingHoursStart:    8,
						WorkingHoursEnd:      18,
						Timezone:             "Europe/Paris",
					},
					AcrLevels: []*corepb.AcrLevel{{Name: "mfa", Require: requirement}},
				}
			},
		},
		"empty messages keep their presence": {
			arrange: func() *corepb.AuthenticationPolicy {
				return &corepb.AuthenticationPolicy{Enrollment: &corepb.EnrollmentPolicy{}}
			},
		},
		"empty": {
			arrange: func() *corepb.AuthenticationPolicy { return &corepb.AuthenticationPolicy{} },
		},
	}

	ctx := context.Background()
	attrs := protoAttributes((&corepb.AuthenticationPolicy{}).ProtoReflect().Descriptor(), nil)
	typ, ok := schema.Schema{Attributes: attrs}.Type().TerraformType(ctx).(tftypes.Object)
	require.True(t, ok)

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {

			in := v.arrange()

			tv := protoToValue(in.ProtoReflect(), typ)

			back := &corepb.AuthenticationPolicy{}
			require.NoError(t, valueToProto(tv, back.ProtoReflect()))

			assert.True(t, proto.Equal(in, back), "expected %v, got %v", in, back)
		})
	}
}

func TestProtoattr_RefusesOutOfRange(t *testing.T) {

	ctx := context.Background()
	attrs := protoAttributes((&corepb.RememberDevicePolicy{}).ProtoReflect().Descriptor(), nil)
	typ, ok := schema.Schema{Attributes: attrs}.Type().TerraformType(ctx).(tftypes.Object)
	require.True(t, ok)

	v := tftypes.NewValue(typ, map[string]tftypes.Value{
		"enabled":     tftypes.NewValue(tftypes.Bool, nil),
		"ttl":         tftypes.NewValue(tftypes.String, nil),
		"max_devices": tftypes.NewValue(tftypes.Number, int64(1)<<40),
	})

	err := valueToProto(v, (&corepb.RememberDevicePolicy{}).ProtoReflect())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_devices")
}

func TestParseDisplayStrings(t *testing.T) {

	type s struct {
		in   string
		want []string
	}

	cases := map[string]s{
		"one value": {
			in:   `%"rule %22admins%22, requires webauthn"`,
			want: []string{`rule "admins", requires webauthn`},
		},
		// A proxy or browser joins repeated header values with ", ", and a
		// warning's own commas must not split it.
		"combined values": {
			in:   `%"first, with a comma", %"second"`,
			want: []string{"first, with a comma", "second"},
		},
		"utf-8 and percent": {
			in:   `%"caf%c3%a9 at 100%25"`,
			want: []string{"café at 100%"},
		},
		"not a display string is kept": {
			in:   `plain warning`,
			want: []string{"plain warning"},
		},
	}

	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			assert.Equal(t, v.want, parseDisplayStrings(v.in))
		})
	}
}

func TestProtoattr_Oneof(t *testing.T) {

	ctx := context.Background()
	md := (&corepb.IssuerConfig{}).ProtoReflect().Descriptor()
	typ, ok := schema.Schema{Attributes: protoAttributes(md, nil)}.Type().TerraformType(ctx).(tftypes.Object)
	require.True(t, ok)

	single := func(realm string) *corepb.IssuerConfig {
		return &corepb.IssuerConfig{ProviderSelector: &corepb.IssuerConfig_SingleRealmProviderSelector{
			SingleRealmProviderSelector: &corepb.SingleRealmProviderSelector{RealmName: realm},
		}}
	}

	t.Run("one member round-trips", func(t *testing.T) {
		in := single("tenants/t-1/realms/r-1")
		back := &corepb.IssuerConfig{}
		require.NoError(t, valueToProto(protoToValue(in.ProtoReflect(), typ), back.ProtoReflect()))
		assert.True(t, proto.Equal(in, back), "expected %v, got %v", in, back)
	})

	t.Run("a second member is refused", func(t *testing.T) {
		v := protoToValue(single("tenants/t-1/realms/r-1").ProtoReflect(), typ)
		var attrs map[string]tftypes.Value
		require.NoError(t, v.As(&attrs))
		ext := typ.AttributeTypes["external_provider_selector"]
		attrs["external_provider_selector"] = tftypes.NewValue(ext, map[string]tftypes.Value{
			"endpoint_name": tftypes.NewValue(tftypes.String, "tenants/t-1/endpoints/e-1"),
		})

		err := valueToProto(tftypes.NewValue(typ, attrs), (&corepb.IssuerConfig{}).ProtoReflect())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "At most one of")
	})
}

func TestProtoattr_Struct(t *testing.T) {

	ctx := context.Background()
	md := (&corepb.PasswordValidatorConfig{}).ProtoReflect().Descriptor()
	typ, ok := schema.Schema{Attributes: protoAttributes(md, nil)}.Type().TerraformType(ctx).(tftypes.Object)
	require.True(t, ok)

	cfg, err := structpb.NewStruct(map[string]any{"min_length": 12.0, "classes": []any{"upper", "digit"}})
	require.NoError(t, err)
	in := &corepb.PasswordValidatorConfig{Name: "length", Type: "length", Config: cfg}

	back := &corepb.PasswordValidatorConfig{}
	require.NoError(t, valueToProto(protoToValue(in.ProtoReflect(), typ), back.ProtoReflect()))
	assert.True(t, proto.Equal(in, back), "expected %v, got %v", in, back)
}

func TestConfigJSON(t *testing.T) {

	ctx := context.Background()

	for _, spec := range configJSONSpecs {
		t.Run(spec.typeName, func(t *testing.T) {
			// Every message's schema builds, and the schema is valid.
			resp := &datasource.SchemaResponse{}
			(&configJSONDataSource{spec: spec}).Schema(ctx, datasource.SchemaRequest{}, resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Empty(t, resp.Schema.ValidateImplementation(ctx))
		})
	}

	t.Run("realm config leaves the policy out", func(t *testing.T) {
		resp := &datasource.SchemaResponse{}
		(&configJSONDataSource{spec: configJSONSpecs[1]}).Schema(ctx, datasource.SchemaRequest{}, resp)
		assert.NotContains(t, resp.Schema.Attributes, authenticationKey)
		assert.Contains(t, resp.Schema.Attributes, "provisioning")
	})

	t.Run("encoding is lowerCamel and without zero values", func(t *testing.T) {
		got, err := configJSON(&corepb.IssuerConfig{
			AccessTokenExpireSeconds: 900,
			ProviderSelector: &corepb.IssuerConfig_MultiRealmProviderSelector{
				MultiRealmProviderSelector: &corepb.MultiRealmProviderSelector{
					RealmNames:    []string{"tenants/t-1/realms/r-1"},
					Identifier:    &corepb.IdentifierField{Kind: corepb.IdentifierField_KIND_UNSPECIFIED},
					DefaultTarget: &corepb.RoutingTarget{RealmName: "tenants/t-1/realms/r-1"},
				},
			},
		})
		require.NoError(t, err)
		assert.JSONEq(t, `{"accessTokenExpireSeconds":900,"multiRealmProviderSelector":{"defaultTarget":{"realmName":"tenants/t-1/realms/r-1"},"identifier":{},"realmNames":["tenants/t-1/realms/r-1"]}}`, got)
	})
}
