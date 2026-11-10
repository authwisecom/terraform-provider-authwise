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
				"Give this issuer its own domain, or its own path on that one (kit#876). "+
				"If the binding is the one kit made for an issuer created with domain_id, import it rather than creating it again.", m["issuer"], where)
		case "DOMAIN_NOT_BINDABLE":
			remedy = "refused: DOMAIN_NOT_BINDABLE. The domain is a Guard address (purpose = \"guard\") or another tenant's; an issuer can be bound only to the tenant's own issuer hosts (purpose = \"issuer\"), native or custom."
		case "CONSOLE_DOMAIN_NOT_BINDABLE":
			remedy = "refused: CONSOLE_DOMAIN_NOT_BINDABLE. The domain is a console address (purpose = \"console\"): it serves the tenant's console, never an issuer, and an issuer's host never serves the console (kit#942). " +
				"Bind the issuer to an issuer host (purpose = \"issuer\", the default) instead, such as login.acme.com beside admin.acme.com."
		default:
			continue
		}
		p := st.Proto()
		p.Message = fmt.Sprintf("%s (%s)", remedy, st.Message())
		return status.FromProto(p).Err()
	}
	return err
}
