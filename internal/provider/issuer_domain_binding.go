package provider

import (
	"context"
	"fmt"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// bindingRefusalClient words kit's refusals of a binding create (kit#911)
// for authwise_issuer_domain_binding, which is generated and reports an
// error as its message only. The remedy goes into the message; the code
// and details stay.
type bindingRefusalClient struct {
	identitypb.AuthwiseIdentityServiceClient
}

func (c bindingRefusalClient) CreateIssuerDomainBinding(ctx context.Context, in *identitypb.CreateIssuerDomainBindingRequest, opts ...grpc.CallOption) (*corepb.IssuerDomainBinding, error) {
	out, err := c.AuthwiseIdentityServiceClient.CreateIssuerDomainBinding(ctx, in, opts...)
	return out, bindingCreateError(err)
}

func bindingCreateError(err error) error {

	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	for _, d := range st.Details() {
		info, isInfo := d.(*errdetails.ErrorInfo)
		if !isInfo || info.GetDomain() != authwiseErrorDomain {
			continue
		}
		var remedy string
		switch info.GetReason() {
		case "BINDING_TAKEN":
			m := info.GetMetadata()
			where := m["hostname"] + m["path"]
			if m["path"] == "" {
				where = m["hostname"] + " at its root"
			}
			remedy = fmt.Sprintf("refused: BINDING_TAKEN. Issuer %s already answers at %s, and one host and path serve one issuer. "+
				"Give this issuer its own domain, or, once kit routes path-prefixed issuers (kit#876), its own path. "+
				"If the binding is the one kit made for an issuer created with domain_id, import it rather than creating it again.", m["issuer"], where)
		case "DOMAIN_NOT_BINDABLE":
			remedy = "refused: DOMAIN_NOT_BINDABLE. The domain is a product's host (product = \"guard\") or another tenant's; an issuer can be bound only to the tenant's own native or custom domains."
		default:
			continue
		}
		p := st.Proto()
		p.Message = fmt.Sprintf("%s (%s)", remedy, st.Message())
		return status.FromProto(p).Err()
	}
	return err
}
