package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/activatedio/tfinfra/pkg/aip"
	tfruntime "github.com/activatedio/tfinfra/pkg/tf"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	messageTypeName = "message"

	// messageChannelEmail is the one channel kit renders today.
	messageChannelEmail = "email"
)

var (
	_ resource.Resource                   = &messageResource{}
	_ resource.ResourceWithConfigure      = &messageResource{}
	_ resource.ResourceWithImportState    = &messageResource{}
	_ resource.ResourceWithValidateConfig = &messageResource{}
)

// messageResource is authwise_message (tf#75, train 2026.11.3 CX6): a
// tenant's own wording of one of kit's messages, for one key, channel and
// locale. What Terraform applies is published; destroying it resets the
// message to kit's built-in.
//
// A message is in one shape at a time. Terraform writes two: subject +
// html + text (the whole mail), and texts (the named parts of a built-in
// message, kit#953). The layout shape is the console's alone (plan, K5):
// a message laid out in the console reads back as its whole mail, which
// differs from the configuration, and apply replaces it.
//
// Its identity is the triple; kit's row id is only how it is addressed.
// Every call to kit goes through messageStore, the one place that changes
// when kit#953's API lands (K1 identity, K2 publish in one call, K3 the
// configuration mark, K4 the shape on the wire).
type messageResource struct {
	store    messageStore
	defaults map[string]string
}

type messageModel struct {
	Name        types.String `tfsdk:"name"`
	TenantID    types.String `tfsdk:"tenant_id"`
	Key         types.String `tfsdk:"key"`
	Channel     types.String `tfsdk:"channel"`
	Locale      types.String `tfsdk:"locale"`
	Texts       types.Map    `tfsdk:"texts"`
	Subject     types.String `tfsdk:"subject"`
	HTML        types.String `tfsdk:"html"`
	Text        types.String `tfsdk:"text"`
	PublishedAt types.String `tfsdk:"published_at"`
	PublishedBy types.String `tfsdk:"published_by"`
}

func newMessageResource() resource.Resource {
	return &messageResource{}
}

func (r *messageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + messageTypeName
}

func (r *messageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {

	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A tenant's own wording of one of kit's messages (an invitation, a password reset, a sign-in code), " +
			"for one `key`, `channel` and `locale`.\n\n" +
			"**What Terraform applies is published**: there is no draft in configuration, and the plan is the review. " +
			"The console shows a message set here as set by configuration and opens it read-only. " +
			"A change made in the console or with awctl all the same shows as drift on the next plan, and apply puts back what is configured.\n\n" +
			"A message takes exactly one shape:\n\n" +
			"- `texts`: the named parts of the built-in message (its subject, heading, body, button and so on). " +
			"A part left out keeps the built-in wording, and the built-in layout renders them, so the brand and spacing stay kit's. " +
			"A part that differs by case is set per case as `\"<part>.<variant>\"`.\n" +
			"- `subject`, `html` and `text`: the whole mail.\n\n" +
			"A value is written in kit's template syntax, `{{ .Recipient.Name }}`; kit refuses a value the message does not offer, and any HTML outside what mail clients allow.\n\n" +
			"A message laid out in the console's layout editor cannot be written here; managing it replaces the layout.\n\n" +
			"**Destroying it resets the message to kit's built-in** and drops its published history.\n\n" +
			"Writing a message needs `identity.messageTemplates.update` at the tenant, which a service account holds through `identity.brandingAdmin`.",
		Attributes: map[string]schema.Attribute{
			tfruntime.NameAttribute: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The message's full resource name (`tenants/{t}/message-templates/{id}`); serves as the Terraform ID.",
				PlanModifiers:       keep,
			},
			tenantIDAttribute: schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The tenant; overrides the provider default. Changing it replaces the resource.",
				PlanModifiers:       replace,
				Validators:          []validator.String{tfruntime.ReferenceID("t", "tenant", "")},
			},
			"key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Which message, as kit names it: `tenant-invitation`, for example. kit lists them, with when each is sent, as message keys. Changing it replaces the resource.",
				PlanModifiers:       replace,
			},
			"channel": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(messageChannelEmail),
				MarkdownDescription: "The channel: `email`, the default and the only one kit renders today. Changing it replaces the resource.",
				PlanModifiers:       replace,
			},
			"locale": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The language, as a BCP 47 tag (`en`, `fr-CA`). A tenant may write a locale kit does not ship, which makes the message available in it. " +
					"Changing it replaces the resource.",
				PlanModifiers: replace,
			},
			"texts": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The named parts of the built-in message, by part name. A part left out keeps the built-in wording. " +
					"Not with `subject`, `html` or `text`.",
			},
			"subject": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The whole mail's subject. With `html` and `text`; not with `texts`.",
			},
			"html": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The whole mail's HTML body. With `subject` and `text`; not with `texts`.",
			},
			"text": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The whole mail's plain-text body, which mail clients show when they show no HTML. With `subject` and `html`; not with `texts`.",
			},
			"published_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When what is live was published, RFC 3339.",
			},
			"published_by": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Who published what is live.",
			},
		},
	}
}

func (r *messageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client := identityClientFrom(req.ProviderData, &resp.Diagnostics)
	if client != nil {
		r.store = messageTemplateStore{client: client}
	}
	if pd, ok := req.ProviderData.(*tfruntime.ProviderData); ok {
		r.defaults = pd.Defaults
	}
}

func (r *messageResource) configured(diags *diag.Diagnostics) bool {
	if r.store == nil {
		diags.AddError("message resource not configured", "Configure was not called with tf.ProviderData")
		return false
	}
	return true
}

// ValidateConfig refuses at plan a message in no shape or in two, and a
// whole mail missing a part.
func (r *messageResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {

	var m messageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.validateShape(&resp.Diagnostics)
}

func (m *messageModel) validateShape(diags *diag.Diagnostics) {

	// An unknown value is a shape still to be decided; it is judged at
	// apply, when every value is known.
	set := func(v interface {
		IsNull() bool
		IsUnknown() bool
	}) bool {
		return !v.IsNull()
	}

	whole := map[string]bool{"subject": set(m.Subject), "html": set(m.HTML), "text": set(m.Text)}
	anyWhole := whole["subject"] || whole["html"] || whole["text"]

	switch {
	case set(m.Texts) && anyWhole:
		diags.AddAttributeError(path.Root("texts"), "a message takes one shape",
			"set either texts (the named parts of the built-in message) or subject, html and text (the whole mail), not both")
	case !set(m.Texts) && !anyWhole:
		diags.AddError("a message takes one shape",
			"set texts (the named parts of the built-in message), or subject, html and text (the whole mail)")
	case anyWhole:
		for _, a := range []string{"subject", "html", "text"} {
			if !whole[a] {
				diags.AddAttributeError(path.Root(a), "the whole mail needs "+a,
					"a message written whole takes subject, html and text together")
			}
		}
	}
}

func (r *messageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan messageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tenant := r.tenant(plan)
	if tenant == "" {
		resp.Diagnostics.AddAttributeError(path.Root(tenantIDAttribute), "missing tenant_id",
			"set tenant_id on the resource or as a provider default")
		return
	}

	content, d := plan.content(ctx)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.identity(tenant)
	existing, err := r.store.find(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("looking for the message failed", err.Error())
		return
	}
	if existing != nil {
		resp.Diagnostics.AddError("the message is already customised",
			fmt.Sprintf("tenant %s already has its own %s message %q in %q (%s). "+
				"Import it to manage it here: terraform import <address> %s, or an import block with that id.",
				tenant, id.channel, id.key, id.locale, existing.name, id.importID()))
		return
	}

	out, err := r.store.publish(ctx, id, "", content)
	if err != nil {
		addMessageWriteError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(plan.fromStored(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *messageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var plan, state messageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	content, d := plan.content(ctx)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.store.publish(ctx, plan.identity(r.tenant(state)), state.Name.ValueString(), content)
	if err != nil {
		addMessageWriteError(&resp.Diagnostics, err)
		return
	}
	resp.Diagnostics.Append(plan.fromStored(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *messageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state messageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var (
		out *storedMessage
		err error
	)
	if name := state.Name.ValueString(); name != "" {
		out, err = r.store.get(ctx, name)
	} else {
		// Just imported: found by its identity.
		out, err = r.store.find(ctx, state.identity(r.tenant(state)))
	}
	if err != nil {
		resp.Diagnostics.AddError("reading the message failed", err.Error())
		return
	}
	// Gone, or holding only a draft nobody published: kit sends the
	// built-in, so the configured wording is not live.
	if out == nil || !out.published {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(state.fromStored(ctx, out)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete resets the message to kit's built-in. A message already gone is
// already reset.
func (r *messageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {

	if !r.configured(&resp.Diagnostics) {
		return
	}

	var state messageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.store.reset(ctx, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("resetting the message to the built-in failed", err.Error())
	}
}

// ImportState takes the message's identity, key/channel/locale, with the
// tenant first when it is not the provider default:
// [tenant_id/]key/channel/locale. Read then finds it; the configured shape
// decides how it is read.
func (r *messageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {

	parts := strings.Split(req.ID, "/")
	if len(parts) == 4 {
		if parts[0] != r.defaults[tenantIDAttribute] {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(tenantIDAttribute), parts[0])...)
		}
		parts = parts[1:]
	}
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("invalid import ID",
			fmt.Sprintf("expected key/channel/locale, or tenant_id/key/channel/locale, such as tenant-invitation/email/en; got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("channel"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("locale"), parts[2])...)
}

func (r *messageResource) tenant(m messageModel) string {
	if v := m.TenantID.ValueString(); v != "" {
		return v
	}
	return r.defaults[tenantIDAttribute]
}

func (m *messageModel) identity(tenant string) messageIdentity {
	return messageIdentity{
		tenant:  tenant,
		key:     m.Key.ValueString(),
		channel: m.Channel.ValueString(),
		locale:  m.Locale.ValueString(),
	}
}

// content is the configured shape, for a write.
func (m *messageModel) content(ctx context.Context) (messageContent, diag.Diagnostics) {

	var diags diag.Diagnostics
	m.validateShape(&diags)
	if diags.HasError() {
		return messageContent{}, diags
	}

	if !m.Texts.IsNull() {
		texts := map[string]string{}
		diags.Append(m.Texts.ElementsAs(ctx, &texts, false)...)
		return messageContent{texts: texts}, diags
	}
	return messageContent{
		subject: m.Subject.ValueString(),
		html:    m.HTML.ValueString(),
		text:    m.Text.ValueString(),
	}, diags
}

// fromStored reads what kit holds into the model. The shape read is the one
// kit reports; a message moved to the other shape outside Terraform reads as
// that shape, so the plan shows the change and apply puts the configured
// one back.
func (m *messageModel) fromStored(ctx context.Context, s *storedMessage) diag.Diagnostics {

	var diags diag.Diagnostics

	m.Name = types.StringValue(s.name)
	m.Key = types.StringValue(s.key)
	m.Channel = types.StringValue(s.channel)
	m.Locale = types.StringValue(s.locale)
	m.PublishedAt = types.StringValue(s.publishedAt)
	m.PublishedBy = types.StringValue(s.publishedBy)

	if s.texts != nil {
		v, d := types.MapValueFrom(ctx, types.StringType, s.texts)
		diags.Append(d...)
		m.Texts = v
		m.Subject, m.HTML, m.Text = types.StringNull(), types.StringNull(), types.StringNull()
		return diags
	}

	m.Texts = types.MapNull(types.StringType)
	m.Subject = types.StringValue(s.subject)
	m.HTML = types.StringValue(s.html)
	m.Text = types.StringValue(s.text)
	return diags
}

// addMessageWriteError explains kit's refusals of a message write.
func addMessageWriteError(diags *diag.Diagnostics, err error) {

	switch status.Code(err) {
	case codes.InvalidArgument:
		diags.AddError("kit refused the message", status.Convert(err).Message()+
			"\n\nkit checks every value a message names against the ones it offers, and its HTML against what mail clients allow.")
	case codes.FailedPrecondition:
		diags.AddError("kit refused the message", status.Convert(err).Message()+
			"\n\nAn install that serves its messages from configuration files takes no message written through the API.")
	case codes.PermissionDenied:
		diags.AddError("writing the message was refused", status.Convert(err).Message()+
			"\n\nWriting a message needs identity.messageTemplates.update at the tenant, which a service account holds through identity.brandingAdmin.")
	default:
		diags.AddError("writing the message failed", err.Error())
	}
}

// messageIdentity is what a message is: its tenant, key, channel and
// locale.
type messageIdentity struct {
	tenant, key, channel, locale string
}

func (id messageIdentity) parent() string { return tenantsCollection + "/" + id.tenant }

func (id messageIdentity) importID() string {
	return strings.Join([]string{id.tenant, id.key, id.channel, id.locale}, "/")
}

// messageContent is one shape of a message: texts set, or the whole mail.
type messageContent struct {
	texts               map[string]string
	subject, html, text string
}

// storedMessage is what kit holds for a message, as the resource reads it.
// texts is non-nil when kit holds the message as named parts.
type storedMessage struct {
	name, key, channel, locale string
	texts                      map[string]string
	subject, html, text        string
	published                  bool
	publishedAt, publishedBy   string
}

// messageStore is every call the resource makes to kit.
type messageStore interface {
	// find is the tenant's message with this identity, or nil.
	find(ctx context.Context, id messageIdentity) (*storedMessage, error)
	// get reads a message by name; nil when it is gone.
	get(ctx context.Context, name string) (*storedMessage, error)
	// publish writes content to the message (name, or a new one when
	// empty) and publishes it.
	publish(ctx context.Context, id messageIdentity, name string, content messageContent) (*storedMessage, error)
	// reset deletes the message, so kit sends its built-in. Already gone
	// is success.
	reset(ctx context.Context, name string) error
}

// messageTemplateStore is messageStore over the MessageTemplate API of
// apis v0.64.0. It has no named parts, no configuration mark and no
// one-call publish, so it writes the draft then publishes it (two calls),
// finds a message by listing, and refuses texts.
type messageTemplateStore struct {
	client identitypb.AuthwiseIdentityServiceClient
}

func (s messageTemplateStore) find(ctx context.Context, id messageIdentity) (*storedMessage, error) {

	token := ""
	for {
		out, err := s.client.ListMessageTemplates(ctx, &identitypb.ListMessageTemplatesRequest{
			Parent:    id.parent(),
			PageSize:  100,
			PageToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, t := range out.GetMessageTemplates() {
			if t.GetKey() == id.key && t.GetChannel() == id.channel && t.GetLocale() == id.locale {
				return storedFromTemplate(t), nil
			}
		}
		token = out.GetNextPageToken()
		if token == "" {
			return nil, nil
		}
	}
}

func (s messageTemplateStore) get(ctx context.Context, name string) (*storedMessage, error) {

	out, err := s.client.GetMessageTemplate(ctx, &identitypb.GetMessageTemplateRequest{Name: name})
	if err != nil {
		if aip.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return storedFromTemplate(out), nil
}

func (s messageTemplateStore) publish(ctx context.Context, id messageIdentity, name string, content messageContent) (*storedMessage, error) {

	if content.texts != nil {
		return nil, status.Error(codes.Unimplemented,
			"texts needs kit's named message parts (kit#953) and the apis release with them; this build writes only subject, html and text")
	}

	tpl := &corepb.MessageTemplate{
		Key:     id.key,
		Channel: id.channel,
		Locale:  id.locale,
		Draft: &corepb.MessageTemplateDraft{
			Subject:  content.subject,
			HtmlBody: content.html,
			TextBody: content.text,
		},
	}

	var (
		saved *corepb.MessageTemplate
		err   error
	)
	if name == "" {
		saved, err = s.client.CreateMessageTemplate(ctx, &identitypb.CreateMessageTemplateRequest{
			Parent:          id.parent(),
			MessageTemplate: tpl,
		})
	} else {
		// The draft alone: a whole-record write would clear what is live
		// until the publish that follows.
		saved, err = s.client.PatchMessageTemplate(ctx, &identitypb.PatchMessageTemplateRequest{
			Name:            name,
			MessageTemplate: &corepb.MessageTemplate{Draft: tpl.GetDraft()},
			UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"draft"}},
		})
	}
	if err != nil {
		return nil, err
	}

	out, err := s.client.PublishMessageTemplate(ctx, &identitypb.PublishMessageTemplateRequest{Name: saved.GetName()})
	if err != nil {
		return nil, fmt.Errorf("%s was saved as a draft but not published: %w", saved.GetName(), err)
	}
	return storedFromTemplate(out.GetMessageTemplate()), nil
}

func (s messageTemplateStore) reset(ctx context.Context, name string) error {

	_, err := s.client.DeleteMessageTemplate(ctx, &identitypb.DeleteMessageTemplateRequest{Name: name})
	if err != nil && !aip.IsNotFound(err) {
		return err
	}
	return nil
}

// storedFromTemplate reads a MessageTemplate as the resource does: what is
// published, never the draft.
func storedFromTemplate(t *corepb.MessageTemplate) *storedMessage {

	s := &storedMessage{
		name:    t.GetName(),
		key:     t.GetKey(),
		channel: t.GetChannel(),
		locale:  t.GetLocale(),
		subject: t.GetSubject(),
		html:    t.GetHtmlBody(),
		text:    t.GetTextBody(),
		// A copy kit published, or one written as text before kit#953,
		// whose parts were live on save; a copy holding only a draft
		// has never been sent.
		published: t.GetPublishedAt() != nil ||
			t.GetSubject() != "" || t.GetHtmlBody() != "" || t.GetTextBody() != "" || t.GetMjml() != "",
		publishedBy: t.GetPublishedBy(),
	}
	if t.GetPublishedAt() != nil {
		s.publishedAt = t.GetPublishedAt().AsTime().UTC().Format(time.RFC3339Nano)
	}
	return s
}
