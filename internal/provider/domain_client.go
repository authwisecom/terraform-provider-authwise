package provider

import (
	"context"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// domainClient is domainAPI over kit's identity service.
type domainClient struct {
	client identitypb.AuthwiseIdentityServiceClient
}

func (c domainClient) CreateDomain(ctx context.Context, parent string, in *domain) (*domain, error) {
	out, err := c.client.CreateDomain(ctx, &identitypb.CreateDomainRequest{
		Parent: parent,
		Domain: &corepb.Domain{Hostname: in.Hostname, IssuerId: in.IssuerID, Labels: in.Labels},
	})
	if err != nil {
		return nil, err
	}
	return domainFromProto(out), nil
}

func (c domainClient) GetDomain(ctx context.Context, name string) (*domain, error) {
	out, err := c.client.GetDomain(ctx, &identitypb.GetDomainRequest{Name: name})
	if err != nil {
		return nil, err
	}
	return domainFromProto(out), nil
}

// FindDomain pages through the tenant's domains for hostname; kit has no
// lookup by hostname.
func (c domainClient) FindDomain(ctx context.Context, parent, hostname string) (*domain, error) {

	token := ""
	for {
		out, err := c.client.ListDomains(ctx, &identitypb.ListDomainsRequest{Parent: parent, PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, d := range out.GetDomains() {
			if d.GetHostname() == hostname {
				return domainFromProto(d), nil
			}
		}
		if token = out.GetNextPageToken(); token == "" {
			return nil, status.Errorf(codes.NotFound, "no domain %s in %s", hostname, parent)
		}
	}
}

// UpdateDomainLabels patches labels alone: kit refuses any other path on a
// custom host.
func (c domainClient) UpdateDomainLabels(ctx context.Context, name string, labels map[string]string) (*domain, error) {
	out, err := c.client.PatchDomain(ctx, &identitypb.PatchDomainRequest{
		Name:       name,
		Domain:     &corepb.Domain{Labels: labels},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{labelsAttribute}},
	})
	if err != nil {
		return nil, err
	}
	return domainFromProto(out), nil
}

func (c domainClient) DeleteDomain(ctx context.Context, name string) error {
	_, err := c.client.DeleteDomain(ctx, &identitypb.DeleteDomainRequest{Name: name})
	return err
}

func domainFromProto(e *corepb.Domain) *domain {
	d := &domain{
		Name:          e.GetName(),
		Hostname:      e.GetHostname(),
		IssuerID:      e.GetIssuerId(),
		Labels:        e.GetLabels(),
		TrafficTarget: e.GetTrafficTarget(),
		ActivatedAt:   timeOf(e.GetActivatedAt()),
	}
	d.setLifecycle(e.GetState(), e.GetFailure(), e.GetCertificateExpiresAt(), e.GetDnsRecords())
	return d
}

// setLifecycle sets what a custom host's Domain and IssuerDomain carry
// alike.
func (d *domain) setLifecycle(state corepb.IssuerDomainState, failure *corepb.IssuerDomainFailure, certificateExpiresAt *timestamppb.Timestamp, records []*corepb.IssuerDomainDnsRecord) {
	d.State = enumName(state.String(), "ISSUER_DOMAIN_STATE_")
	if failure != nil {
		d.FailureCode = enumName(failure.GetCode().String(), "ISSUER_DOMAIN_FAILURE_CODE_")
		d.FailureMessage = failure.GetMessage()
		d.FailureRecord = failure.GetRecord()
	}
	d.CertificateExpiresAt = timeOf(certificateExpiresAt)
	for _, r := range records {
		d.DNSRecords = append(d.DNSRecords, domainRecord{Name: r.GetName(), Type: r.GetType(), Value: r.GetValue(), Purpose: r.GetPurpose()})
	}
}

func timeOf(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}
