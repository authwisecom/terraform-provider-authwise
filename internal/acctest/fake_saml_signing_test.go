package acctest_test

import (
	"fmt"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
)

// The SAML signing requirements' admission from kit v1.100.0 (K-34,
// kit#1041, apis v0.91.0): kit pkg/lib/model/saml_signing.go. The three
// switches are proto3 optional and unset reads as true, so the fake never
// refuses one the configuration left out. Only an explicit false lowers a
// requirement, and only beside the other signature, named.

// admitProviderSamlSigning refuses an explicit want_assertions_signed:
// false unless want_response_signed is true.
func admitProviderSamlSigning(config *anypb.Any) error {
	saml := &corepb.ProviderSaml{}
	if !config.MessageIs(saml) {
		return nil
	}
	if err := config.UnmarshalTo(saml); err != nil {
		return status.Errorf(codes.InvalidArgument, "config: %v", err)
	}
	if saml.WantAssertionsSigned == nil || saml.GetWantAssertionsSigned() || saml.GetWantResponseSigned() {
		return nil
	}
	return status.Error(codes.InvalidArgument, "want_assertions_signed: false would leave the assertion unverified and "+
		"want_response_signed is not set, so kit would accept an assertion nobody signed: "+
		"set want_response_signed: true, or leave want_assertions_signed unset")
}

// admitClientSamlSigning refuses an explicit sign_response: false unless
// sign_assertions is explicitly true. sign_assertions: false beside a
// signed Response is accepted.
func admitClientSamlSigning(config *anypb.Any) error {
	rp := &corepb.SamlRelyingPartyConfig{}
	if !config.MessageIs(rp) {
		return nil
	}
	if err := config.UnmarshalTo(rp); err != nil {
		return status.Errorf(codes.InvalidArgument, "config: %v", err)
	}
	if rp.SignResponse == nil || rp.GetSignResponse() || rp.GetSignAssertions() {
		return nil
	}
	state := "is not set"
	if rp.SignAssertions != nil {
		state = "is false"
	}
	return status.Error(codes.InvalidArgument, fmt.Sprintf("sign_response: false would leave the Response unsigned and sign_assertions %s, "+
		"so nothing kit sends would be signed (encrypt_assertions is not a signature): "+
		"set sign_assertions: true, or leave sign_response unset", state))
}
