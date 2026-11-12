package acctest_test

import (
	"context"
	"maps"
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

// The fake's message templates (tf#75), as kit#953 holds them (apis
// v0.65.0): a copy per key, channel and locale. :apply writes it live in
// one call, with its managed_by mark; Create and Patch write drafts only;
// :publish takes a draft live and clears the mark; Delete is the reset to
// the built-in. texts are checked against each key's parts, as kit#953's
// part catalog checks them (apis v0.66.0).

// fakeMessagePublisher is who the fake records as publishing.
const fakeMessagePublisher = "c-acc"

// fakeMessageParts are each key's email parts and their cases, as kit
// lists them (kit#953): not the same on every key. The fake ships wording
// in en only.
var fakeMessageParts = map[string]map[string][]string{
	"tenant-invitation": {
		"subject": {"administration", "join"}, "greeting": nil, "body": {"administration", "join"},
		"button": nil, "instructions": {"link_only", "password"}, "details": nil, "note": nil,
	},
	"mfa-otp": {
		"subject": {"enrolment", "sign_in"}, "greeting": nil, "body": {"enrolment", "sign_in"},
		"details": nil, "note": {"enrolment", "sign_in"},
	},
}

// fakeShippedLocale is the one locale the fake ships wording in.
const fakeShippedLocale = "en"

// ListMessageKeys lists the fake's keys with their parts.
func (f *fakeIdentityServer) ListMessageKeys(_ context.Context, _ *identitypb.ListMessageKeysRequest) (*identitypb.ListMessageKeysResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listKeys++
	if f.keysUnavailable {
		return nil, status.Error(codes.Unavailable, "message keys unavailable")
	}
	out := &identitypb.ListMessageKeysResponse{}
	for _, key := range slices.Sorted(maps.Keys(fakeMessageParts)) {
		mk := &identitypb.MessageKeyMetadata{Key: key, Channels: []string{"email"}, Locales: []string{fakeShippedLocale}}
		for _, name := range slices.Sorted(maps.Keys(fakeMessageParts[key])) {
			p := &identitypb.MessagePart{Name: name, Channel: "email"}
			for _, v := range fakeMessageParts[key][name] {
				p.Variants = append(p.Variants, &identitypb.MessagePartVariant{Name: v})
			}
			mk.Parts = append(mk.Parts, p)
		}
		out.MessageKeys = append(out.MessageKeys, mk)
	}
	return out, nil
}

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

// ApplyMessageTemplate is kit#953's one call for configuration: upsert by
// the triple, live, draft cleared, published and marked.
func (f *fakeIdentityServer) ApplyMessageTemplate(ctx context.Context, req *identitypb.ApplyMessageTemplateRequest) (*identitypb.ApplyMessageTemplateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)
	f.applies++

	channel := req.GetChannel()
	if channel == "" {
		channel = "email"
	}
	if err := admitMessageIdentity(req.GetKey(), channel); err != nil {
		return nil, err
	}
	whole := req.GetSubject() != "" || req.GetHtmlBody() != "" || req.GetTextBody() != ""
	if len(req.GetTexts()) > 0 && whole {
		return nil, status.Error(codes.InvalidArgument, "texts: set texts, or subject, text_body and html_body; not both")
	}
	if err := admitMessageTexts(req.GetKey(), req.GetLocale(), req.GetTexts()); err != nil {
		return nil, err
	}
	if err := admitMessageValues(req.GetSubject(), req.GetHtmlBody(), req.GetTextBody()); err != nil {
		return nil, err
	}

	t := f.findMessage(req.GetParent(), req.GetKey(), channel, req.GetLocale())
	created := t == nil
	if created {
		t = &corepb.MessageTemplate{
			Name: req.GetParent() + "/message-templates/" + f.nextID("mt"),
			Key:  req.GetKey(), Channel: channel, Locale: req.GetLocale(),
		}
		f.messages[t.GetName()] = t
	}
	t.Texts, t.Subject, t.HtmlBody, t.TextBody = nil, "", "", ""
	t.Blocks, t.Mjml, t.Draft = nil, "", nil
	if len(req.GetTexts()) > 0 {
		t.Texts = req.GetTexts()
		t.Shape = corepb.MessageShape_MESSAGE_SHAPE_TEXTS
	} else {
		t.Subject, t.HtmlBody, t.TextBody = req.GetSubject(), req.GetHtmlBody(), req.GetTextBody()
		t.Shape = corepb.MessageShape_MESSAGE_SHAPE_HTML
	}
	t.ManagedBy = req.GetManagedBy()
	f.stampPublished(t)
	return &identitypb.ApplyMessageTemplateResponse{MessageTemplate: proto.Clone(t).(*corepb.MessageTemplate), Created: created}, nil
}

// CreateMessageTemplate writes a copy holding a draft; a live part is
// refused, as kit#953 refuses it.
func (f *fakeIdentityServer) CreateMessageTemplate(ctx context.Context, req *identitypb.CreateMessageTemplateRequest) (*corepb.MessageTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	t := proto.Clone(req.GetMessageTemplate()).(*corepb.MessageTemplate)
	if err := admitMessageIdentity(t.GetKey(), t.GetChannel()); err != nil {
		return nil, err
	}
	if t.GetSubject() != "" || t.GetHtmlBody() != "" || t.GetTextBody() != "" || len(t.GetTexts()) > 0 {
		return nil, status.Error(codes.InvalidArgument, "a live part is written by :publish or :apply; save a draft")
	}
	t.Name = req.GetParent() + "/message-templates/" + f.nextID("mt")
	t.PublishedAt, t.PublishedBy, t.Shape = nil, "", corepb.MessageShape_MESSAGE_SHAPE_UNSPECIFIED
	f.messages[t.GetName()] = t
	return proto.Clone(t).(*corepb.MessageTemplate), nil
}

// PatchMessageTemplate writes the draft only.
func (f *fakeIdentityServer) PatchMessageTemplate(ctx context.Context, req *identitypb.PatchMessageTemplateRequest) (*corepb.MessageTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordAuth(ctx)

	t, ok := f.messages[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s not found", req.GetName())
	}
	for _, p := range req.GetUpdateMask().GetPaths() {
		if p != "draft" {
			return nil, status.Errorf(codes.InvalidArgument, "update_mask: %q is written by :publish or :apply", p)
		}
		t.Draft = proto.Clone(req.GetMessageTemplate().GetDraft()).(*corepb.MessageTemplateDraft)
	}
	return proto.Clone(t).(*corepb.MessageTemplate), nil
}

// PublishMessageTemplate is the console's publish: the draft goes live and
// the configuration mark is cleared.
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
	t.Subject, t.TextBody, t.HtmlBody, t.Texts = d.GetSubject(), d.GetTextBody(), d.GetHtmlBody(), d.GetTexts()
	t.Blocks, t.Mjml = d.GetBlocks(), d.GetMjml()
	switch {
	case d.GetBlocks() != nil:
		t.Shape = corepb.MessageShape_MESSAGE_SHAPE_LAYOUT
	case len(d.GetTexts()) > 0:
		t.Shape = corepb.MessageShape_MESSAGE_SHAPE_TEXTS
	default:
		t.Shape = corepb.MessageShape_MESSAGE_SHAPE_HTML
	}
	t.Draft, t.ManagedBy = nil, ""
	f.stampPublished(t)
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

func (f *fakeIdentityServer) stampPublished(t *corepb.MessageTemplate) {
	f.seq++
	t.PublishedAt = timestamppb.New(certNotBefore.Add(time.Duration(f.seq) * time.Second))
	t.PublishedBy = fakeMessagePublisher
}

// findMessage is the stored copy with this identity, or nil. The caller
// holds the lock.
func (f *fakeIdentityServer) findMessage(parent, key, channel, locale string) *corepb.MessageTemplate {
	for _, t := range f.messages {
		if strings.HasPrefix(t.GetName(), parent+"/") && t.GetKey() == key && t.GetChannel() == channel && t.GetLocale() == locale {
			return t
		}
	}
	return nil
}

func admitMessageIdentity(key, channel string) error {
	if _, ok := fakeMessageParts[key]; !ok {
		return status.Errorf(codes.InvalidArgument, "key %q is not a message kit sends", key)
	}
	if channel != "email" {
		return status.Errorf(codes.InvalidArgument, "channel %q: only email renders", channel)
	}
	return nil
}

// admitMessageTexts is kit's check of a copy's parts: an unknown part or
// case is refused by name, a subject is one line, and a locale kit ships no
// wording for needs every part.
func admitMessageTexts(key, locale string, texts map[string]string) error {
	if len(texts) == 0 {
		return nil
	}
	parts := fakeMessageParts[key]
	set := map[string]bool{}
	for name, v := range texts {
		part, variant, cased := strings.Cut(name, ".")
		cases, ok := parts[part]
		switch {
		case !ok:
			return status.Errorf(codes.InvalidArgument, "texts: %q is not a part of %s", name, key)
		case cased && !slices.Contains(cases, variant):
			return status.Errorf(codes.InvalidArgument, "texts: %q: part %s has no case %q", name, part, variant)
		case part == "subject" && strings.ContainsAny(v, "\r\n"):
			return status.Errorf(codes.InvalidArgument, "texts: %q: a subject is one line", name)
		}
		if err := admitMessageValues(v); err != nil {
			return err
		}
		set[part] = true
	}
	if locale != fakeShippedLocale {
		for part := range parts {
			if !set[part] {
				return status.Errorf(codes.InvalidArgument, "texts: %q: kit ships no %s wording for %s, so every part must be set", part, locale, key)
			}
		}
	}
	return nil
}

// admitMessageValues refuses a value the message does not offer, as kit's
// save-time render does: the fake refuses .Unknown.
func admitMessageValues(sources ...string) error {
	for _, s := range sources {
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
	if t := f.findMessage("tenants/"+tenant, key, channel, locale); t != nil {
		return proto.Clone(t).(*corepb.MessageTemplate)
	}
	return nil
}

// editMessage changes a stored copy behind Terraform's back.
func (f *fakeIdentityServer) editMessage(name string, edit func(*corepb.MessageTemplate)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	edit(f.messages[name])
}
