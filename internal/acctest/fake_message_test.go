package acctest_test

import (
	"context"
	"slices"
	"strings"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The fake's message templates (tf#75), as kit develop holds them before
// kit#953: a row per key, channel and locale, written as a draft and made
// live by :publish, which stamps published_at and published_by. Delete is
// "reset to default".

// fakeMessagePublisher is who the fake records as publishing.
const fakeMessagePublisher = "c-acc"

// fakeMessageKeys are the keys the fake's catalog offers; anything else is
// refused at save, as kit refuses it.
var fakeMessageKeys = []string{"tenant-invitation", "password-recovery"}

func (f *fakeIdentityServer) ListMessageTemplates(ctx context.Context, req *identitypb.ListMessageTemplatesRequest) (*identitypb.ListMessageTemplatesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	page, next := pageUnder(f.messages, req.GetParent()+"/message-templates/", req.GetPageToken())
	return &identitypb.ListMessageTemplatesResponse{MessageTemplates: page, NextPageToken: next}, nil
}

func (f *fakeIdentityServer) GetMessageTemplate(ctx context.Context, req *identitypb.GetMessageTemplateRequest) (*corepb.MessageTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	t, ok := f.messages[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", req.GetName())
	}
	return proto.Clone(t).(*corepb.MessageTemplate), nil
}

func (f *fakeIdentityServer) CreateMessageTemplate(ctx context.Context, req *identitypb.CreateMessageTemplateRequest) (*corepb.MessageTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	t := proto.Clone(req.GetMessageTemplate()).(*corepb.MessageTemplate)
	if !slices.Contains(fakeMessageKeys, t.GetKey()) {
		return nil, status.Errorf(codes.InvalidArgument, "key %q is not a message kit sends", t.GetKey())
	}
	if t.GetChannel() != "email" {
		return nil, status.Errorf(codes.InvalidArgument, "channel %q: only email renders", t.GetChannel())
	}
	if err := admitMessageDraft(t.GetDraft()); err != nil {
		return nil, err
	}
	t.Name = req.GetParent() + "/message-templates/" + f.nextID("mt")
	t.PublishedAt, t.PublishedBy = nil, ""
	f.messages[t.GetName()] = t
	return proto.Clone(t).(*corepb.MessageTemplate), nil
}

// PatchMessageTemplate takes the masks the provider sends: draft, and the
// live parts a console edit writes.
func (f *fakeIdentityServer) PatchMessageTemplate(ctx context.Context, req *identitypb.PatchMessageTemplateRequest) (*corepb.MessageTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	t, ok := f.messages[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", req.GetName())
	}
	in := req.GetMessageTemplate()
	for _, p := range req.GetUpdateMask().GetPaths() {
		switch p {
		case "draft":
			if err := admitMessageDraft(in.GetDraft()); err != nil {
				return nil, err
			}
			t.Draft = proto.Clone(in.GetDraft()).(*corepb.MessageTemplateDraft)
		case "subject":
			t.Subject = in.GetSubject()
		case "html_body":
			t.HtmlBody = in.GetHtmlBody()
		case "text_body":
			t.TextBody = in.GetTextBody()
		default:
			return nil, status.Errorf(codes.InvalidArgument, "update_mask: the fake takes no %q", p)
		}
	}
	return proto.Clone(t).(*corepb.MessageTemplate), nil
}

func (f *fakeIdentityServer) PublishMessageTemplate(ctx context.Context, req *identitypb.PublishMessageTemplateRequest) (*identitypb.PublishMessageTemplateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	t, ok := f.messages[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", req.GetName())
	}
	d := t.GetDraft()
	if d == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%s has no draft to publish", req.GetName())
	}
	t.Subject, t.TextBody, t.HtmlBody = d.GetSubject(), d.GetTextBody(), d.GetHtmlBody()
	t.Blocks, t.Mjml = d.GetBlocks(), d.GetMjml()
	t.Draft = nil
	f.seq++
	t.PublishedAt = timestamppb.New(certNotBefore.Add(time.Duration(f.seq) * time.Second))
	t.PublishedBy = fakeMessagePublisher
	return &identitypb.PublishMessageTemplateResponse{MessageTemplate: proto.Clone(t).(*corepb.MessageTemplate)}, nil
}

func (f *fakeIdentityServer) DeleteMessageTemplate(ctx context.Context, req *identitypb.DeleteMessageTemplateRequest) (*emptypb.Empty, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	if _, ok := f.messages[req.GetName()]; !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", req.GetName())
	}
	delete(f.messages, req.GetName())
	return &emptypb.Empty{}, nil
}

// admitMessageDraft refuses a value the message does not offer, as kit's
// save-time render does: the fake refuses .Unknown.
func admitMessageDraft(d *corepb.MessageTemplateDraft) error {
	for _, s := range []string{d.GetSubject(), d.GetHtmlBody(), d.GetTextBody()} {
		if strings.Contains(s, "{{ .Unknown") {
			return status.Error(codes.InvalidArgument, `template: can't evaluate field Unknown`)
		}
	}
	return nil
}

// message is the tenant's stored message with this identity, or nil.
func (f *fakeIdentityServer) message(tenant, key, channel, locale string) *corepb.MessageTemplate {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.messages {
		if strings.HasPrefix(t.GetName(), "tenants/"+tenant+"/") && t.GetKey() == key && t.GetChannel() == channel && t.GetLocale() == locale {
			return proto.Clone(t).(*corepb.MessageTemplate)
		}
	}
	return nil
}

// editMessage writes live parts behind Terraform's back, as a console
// edit published there does.
func (f *fakeIdentityServer) editMessage(name string, edit func(*corepb.MessageTemplate)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	edit(f.messages[name])
}
