package provider

import (
	"context"
	"fmt"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// issuerDeleteClient says what to do when kit refuses to delete an issuer
// that still has custom domains (ISSUER_HAS_DOMAINS, kit#796). The generated
// issuer resource reports a delete error as its message only, so the remedy
// goes into the message here, on the way back; the code and details stay.
type issuerDeleteClient struct {
	identitypb.AuthwiseIdentityServiceClient
}

func (c issuerDeleteClient) DeleteIssuer(ctx context.Context, in *identitypb.DeleteIssuerRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	out, err := c.AuthwiseIdentityServiceClient.DeleteIssuer(ctx, in, opts...)
	return out, issuerDeleteError(err)
}

func issuerDeleteError(err error) error {

	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	for _, d := range st.Details() {
		info, isInfo := d.(*errdetails.ErrorInfo)
		if !isInfo || info.GetDomain() != authwiseErrorDomain || info.GetReason() != "ISSUER_HAS_DOMAINS" {
			continue
		}
		p := st.Proto()
		p.Message = fmt.Sprintf("refused: ISSUER_HAS_DOMAINS. The issuer still has %s custom domain(s), such as %s, counting any still being deleted. "+
			"Destroy its authwise_issuer_domain resources first and let them finish: a domain whose issuer_id is set from this issuer is destroyed first "+
			"and waited for; with a literal issuer_id, add depends_on naming the issuer. (%s)",
			info.GetMetadata()["count"], info.GetMetadata()["hostname"], st.Message())
		return status.FromProto(p).Err()
	}
	return err
}
