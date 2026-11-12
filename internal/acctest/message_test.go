package acctest_test

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"testing"

	identitypb "git.authwise.com/authwise/apis/authwise/identity/v1alpha1"
	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/protobuf/types/known/structpb"
)

const messageAddr = "authwise_message.invitation"

const (
	wholeHTML = `<p>Hello {{ .Recipient.DisplayName }}, <a href="{{ .Invitation.AcceptURL }}">join us</a>.</p>`
	wholeText = `Hello {{ .Recipient.DisplayName }}, join us: {{ .Invitation.AcceptURL }}`
)

// wholeMessage is the invitation written whole, with subject s.
func (h *harness) wholeMessage(subject string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_message" "invitation" {
  key    = "tenant-invitation"
  locale = "en"

  subject = %q
  html    = %q
  text    = %q
}
`, subject, wholeHTML, wholeText)
}

// textsMessage is the invitation as named parts.
func (h *harness) textsMessage(texts string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_message" "invitation" {
  key    = "tenant-invitation"
  locale = "en"

  texts = %s
}
`, texts)
}

// liveMessage asserts what kit sends for the invitation: the shape and its
// content, published in one call with the configuration mark, no draft.
func (h *harness) liveMessage(shape corepb.MessageShape, subject string, texts map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		m := h.fake.message("t-1", "tenant-invitation", "email", "en")
		switch {
		case m == nil:
			return fmt.Errorf("no message stored")
		case m.GetShape() != shape:
			return fmt.Errorf("shape = %v, want %v", m.GetShape(), shape)
		case m.GetSubject() != subject:
			return fmt.Errorf("live subject = %q, want %q", m.GetSubject(), subject)
		case !maps.Equal(m.GetTexts(), texts):
			return fmt.Errorf("live texts = %v, want %v", m.GetTexts(), texts)
		case m.GetDraft() != nil:
			return fmt.Errorf("a draft was left: %v", m.GetDraft())
		case m.GetManagedBy() != "terraform":
			return fmt.Errorf("managed_by = %q", m.GetManagedBy())
		case m.GetPublishedAt() == nil || m.GetPublishedBy() != fakeMessagePublisher:
			return fmt.Errorf("not published: %v", m)
		}
		return nil
	}
}

// consolePublish publishes a draft from the console over the invitation,
// which clears the configuration mark.
func (h *harness) consolePublish(draft *corepb.MessageTemplateDraft) func() {
	return func() {
		m := h.fake.message("t-1", "tenant-invitation", "email", "en")
		h.fake.editMessage(m.GetName(), func(t *corepb.MessageTemplate) { t.Draft = draft })
		if _, err := h.fake.PublishMessageTemplate(context.Background(), &identitypb.PublishMessageTemplateRequest{Name: m.GetName()}); err != nil {
			panic(err)
		}
	}
}

func expectAction(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(messageAddr, action)}}
}

// TestAccMessage_WholeMailLifecycle (tf#75): a message written whole is
// applied live in one call on create and on change; a console publish, of
// the same words, of other words, or of a layout, shows as drift that apply
// puts back, mark included; a message deleted behind Terraform's back is
// written again; import by key/channel/locale plans nothing; destroy resets
// it to the built-in.
func TestAccMessage_WholeMailLifecycle(t *testing.T) {

	h := newHarness(t)
	html := corepb.MessageShape_MESSAGE_SHAPE_HTML

	var firstPublish string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.wholeMessage("Join us"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(messageAddr, "name", regexp.MustCompile(`^tenants/t-1/message-templates/mt-\d+$`)),
					resource.TestCheckResourceAttr(messageAddr, "channel", "email"),
					resource.TestCheckResourceAttr(messageAddr, "managed_by", "terraform"),
					resource.TestCheckNoResourceAttr(messageAddr, "tenant_id"),
					resource.TestCheckNoResourceAttr(messageAddr, "texts"),
					resource.TestCheckResourceAttr(messageAddr, "published_by", fakeMessagePublisher),
					func(s *terraform.State) error {
						firstPublish = s.RootModule().Resources[messageAddr].Primary.Attributes["published_at"]
						if h.fake.applies != 1 {
							return fmt.Errorf("%d applies, want 1", h.fake.applies)
						}
						return nil
					},
					h.liveMessage(html, "Join us", nil),
				),
			},
			{Config: h.wholeMessage("Join us"), PlanOnly: true},
			{
				Config:           h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					h.liveMessage(html, "Join Northwind", nil),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[messageAddr].Primary.Attributes["published_at"]; got == firstPublish {
							return fmt.Errorf("published_at did not move: %s", got)
						}
						return nil
					},
				),
			},
			{
				// Published again from the console with the same words:
				// only the mark is gone, and that is drift.
				PreConfig:        h.consolePublish(&corepb.MessageTemplateDraft{Subject: "Join Northwind", HtmlBody: wholeHTML, TextBody: wholeText}),
				Config:           h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            h.liveMessage(html, "Join Northwind", nil),
			},
			{
				// Other words from the console.
				PreConfig:        h.consolePublish(&corepb.MessageTemplateDraft{Subject: "Edited in the console", HtmlBody: wholeHTML, TextBody: wholeText}),
				Config:           h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            h.liveMessage(html, "Join Northwind", nil),
			},
			{
				// Laid out in the console: replaced by the configured mail.
				PreConfig: h.consolePublish(&corepb.MessageTemplateDraft{
					Blocks: &structpb.Struct{}, Mjml: "<mjml/>", Subject: "Join Northwind", HtmlBody: "<table>compiled</table>", TextBody: wholeText,
				}),
				Config:           h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					h.liveMessage(html, "Join Northwind", nil),
					func(*terraform.State) error {
						if m := h.fake.message("t-1", "tenant-invitation", "email", "en"); m.GetBlocks() != nil || m.GetMjml() != "" {
							return fmt.Errorf("the layout was kept: %v", m)
						}
						return nil
					},
				),
			},
			{
				// Reset behind Terraform's back: written again.
				PreConfig: func() {
					m := h.fake.message("t-1", "tenant-invitation", "email", "en")
					h.fake.mu.Lock()
					delete(h.fake.messages, m.GetName())
					h.fake.mu.Unlock()
				},
				Config:           h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionCreate),
				Check:            h.liveMessage(html, "Join Northwind", nil),
			},
			{
				ResourceName:    messageAddr,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   "tenant-invitation/email/en",
				Config:          h.wholeMessage("Join Northwind"),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if m := h.fake.message("t-1", "tenant-invitation", "email", "en"); m != nil {
				return fmt.Errorf("not reset to the built-in: %s is still stored", m.GetName())
			}
			return nil
		},
	})
}

// TestAccMessage_Texts: named parts, a part set per case, read back as
// written; moving between the two shapes is an update in place; an unknown
// part is refused by kit, by name.
func TestAccMessage_Texts(t *testing.T) {

	h := newHarness(t)
	texts := map[string]string{
		"subject":   "Welcome to {{ .Brand.DisplayName }}",
		"body.join": "Your team uses {{ .Brand.DisplayName }} to sign in.",
		"button":    "Accept",
	}
	textsHCL := `{
    subject     = "Welcome to {{ .Brand.DisplayName }}"
    "body.join" = "Your team uses {{ .Brand.DisplayName }} to sign in."
    button      = "Accept"
  }`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.textsMessage(textsHCL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(messageAddr, "texts.%", "3"),
					resource.TestCheckResourceAttr(messageAddr, "texts.body.join", texts["body.join"]),
					resource.TestCheckNoResourceAttr(messageAddr, "subject"),
					h.liveMessage(corepb.MessageShape_MESSAGE_SHAPE_TEXTS, "", texts),
				),
			},
			{Config: h.textsMessage(textsHCL), PlanOnly: true},
			{
				Config:           h.wholeMessage("Join us"),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            h.liveMessage(corepb.MessageShape_MESSAGE_SHAPE_HTML, "Join us", nil),
			},
			{
				Config:           h.textsMessage(textsHCL),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            h.liveMessage(corepb.MessageShape_MESSAGE_SHAPE_TEXTS, "", texts),
			},
			{
				Config:      h.textsMessage(`{ heading = "Hi" }`),
				ExpectError: regexp.MustCompile(`(?s)kit refused the message.*"heading" is not a part`),
			},
		},
	})
}

// TestAccMessage_AlreadyCustomised: a message the console already
// customised is not overwritten by a create; the error names the import.
// Imported with the tenant in the id, the first apply only marks it.
func TestAccMessage_AlreadyCustomised(t *testing.T) {

	h := newHarness(t)
	seeded := func() {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		h.fake.messages["tenants/t-1/message-templates/mt-console"] = &corepb.MessageTemplate{
			Name: "tenants/t-1/message-templates/mt-console", Key: "tenant-invitation", Channel: "email", Locale: "en",
			Shape: corepb.MessageShape_MESSAGE_SHAPE_HTML, Subject: "Join us", HtmlBody: wholeHTML, TextBody: wholeText,
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				PreConfig:   seeded,
				Config:      h.wholeMessage("Join us"),
				ExpectError: regexp.MustCompile(`(?s)already customised.*t-1/tenant-invitation/email/en`),
			},
			{
				Config: h.wholeMessage("Join us") + `
import {
  to = authwise_message.invitation
  id = "t-1/tenant-invitation/email/en"
}
`,
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(messageAddr, "name", "tenants/t-1/message-templates/mt-console"),
					h.liveMessage(corepb.MessageShape_MESSAGE_SHAPE_HTML, "Join us", nil),
				),
			},
			{Config: h.wholeMessage("Join us"), PlanOnly: true},
		},
	})
}

// TestAccMessage_Refusals: a message in no shape, two shapes, a whole mail
// missing a part, or a mark kit would refuse, is refused at plan; a value
// the message does not offer is refused by kit at apply.
func TestAccMessage_Refusals(t *testing.T) {

	h := newHarness(t)
	cfg := func(body string) string {
		return h.providerConfig() + `
resource "authwise_message" "invitation" {
  key    = "tenant-invitation"
  locale = "en"
` + body + "\n}\n"
	}
	whole := `html = "<p>Hi</p>"` + "\n" + `text = "Hi"`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: cfg(``), PlanOnly: true, ExpectError: regexp.MustCompile(`a message takes one shape`)},
			{
				Config:      cfg(`texts = { body = "Hi" }` + "\n" + `subject = "Hi"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not both`),
			},
			{Config: cfg(`subject = "Hi"`), PlanOnly: true, ExpectError: regexp.MustCompile(`the whole mail needs html`)},
			{
				Config:      cfg(`subject = "Hi"` + "\n" + whole + "\n" + `managed_by = "Terraform"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`a-z, 0-9 and -`),
			},
			{
				Config:      cfg(`subject = "Hi {{ .Unknown }}"` + "\n" + whole),
				ExpectError: regexp.MustCompile(`(?s)kit refused the message.*can't evaluate field Unknown`),
			},
		},
	})
}
