package acctest_test

import (
	"fmt"
	"regexp"
	"testing"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const messageAddr = "authwise_message.invitation"

// wholeMessage is the invitation written whole, with subject s.
func (h *harness) wholeMessage(subject string) string {
	return h.providerConfig() + fmt.Sprintf(`
resource "authwise_message" "invitation" {
  key    = "tenant-invitation"
  locale = "en"

  subject = %q
  html    = "<p>Hello {{ .Recipient.DisplayName }}, <a href=\"{{ .Invitation.AcceptURL }}\">join us</a>.</p>"
  text    = "Hello {{ .Recipient.DisplayName }}, join us: {{ .Invitation.AcceptURL }}"
}
`, subject)
}

// TestAccMessage_WholeMailLifecycle (tf#75): a message written whole is
// published on create and on change; an edit made in the console shows as
// drift and apply puts the configuration back; a message deleted behind
// Terraform's back is written again; import by key/channel/locale plans
// nothing; destroy resets it to the built-in.
func TestAccMessage_WholeMailLifecycle(t *testing.T) {

	h := newHarness(t)

	// live asserts what kit sends: the subject published, no draft left.
	live := func(subject string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			m := h.fake.message("t-1", "tenant-invitation", "email", "en")
			switch {
			case m == nil:
				return fmt.Errorf("no message stored")
			case m.GetSubject() != subject:
				return fmt.Errorf("live subject = %q, want %q", m.GetSubject(), subject)
			case m.GetDraft() != nil:
				return fmt.Errorf("a draft was left: %v", m.GetDraft())
			case m.GetPublishedAt() == nil || m.GetPublishedBy() != fakeMessagePublisher:
				return fmt.Errorf("not published: %v", m)
			}
			return nil
		}
	}

	var firstPublish string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: h.wholeMessage("Join us"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(messageAddr, "name", regexp.MustCompile(`^tenants/t-1/message-templates/mt-\d+$`)),
					resource.TestCheckResourceAttr(messageAddr, "channel", "email"),
					resource.TestCheckNoResourceAttr(messageAddr, "tenant_id"),
					resource.TestCheckNoResourceAttr(messageAddr, "texts"),
					resource.TestCheckResourceAttr(messageAddr, "published_by", fakeMessagePublisher),
					func(s *terraform.State) error {
						firstPublish = s.RootModule().Resources[messageAddr].Primary.Attributes["published_at"]
						return nil
					},
					live("Join us"),
				),
			},
			{Config: h.wholeMessage("Join us"), PlanOnly: true},
			{
				// Changed in configuration: published again, in place.
				Config: h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(messageAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					live("Join Northwind"),
					func(s *terraform.State) error {
						if got := s.RootModule().Resources[messageAddr].Primary.Attributes["published_at"]; got == firstPublish {
							return fmt.Errorf("published_at did not move: %s", got)
						}
						return nil
					},
				),
			},
			{
				// Edited and published in the console: drift, and apply
				// puts the configured wording back.
				PreConfig: func() {
					m := h.fake.message("t-1", "tenant-invitation", "email", "en")
					h.fake.editMessage(m.GetName(), func(t *corepb.MessageTemplate) { t.Subject = "Edited in the console" })
				},
				Config: h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(messageAddr, plancheck.ResourceActionUpdate)},
				},
				Check: live("Join Northwind"),
			},
			{
				// Reset behind Terraform's back: written again.
				PreConfig: func() {
					m := h.fake.message("t-1", "tenant-invitation", "email", "en")
					h.fake.mu.Lock()
					delete(h.fake.messages, m.GetName())
					h.fake.mu.Unlock()
				},
				Config: h.wholeMessage("Join Northwind"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(messageAddr, plancheck.ResourceActionCreate)},
				},
				Check: live("Join Northwind"),
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

// TestAccMessage_AlreadyCustomised: a message the console already
// customised is not overwritten by a create; the error names the import.
// Imported with the tenant in the id, it plans nothing when the wording
// matches.
func TestAccMessage_AlreadyCustomised(t *testing.T) {

	h := newHarness(t)
	seeded := func() {
		h.fake.mu.Lock()
		defer h.fake.mu.Unlock()
		h.fake.messages["tenants/t-1/message-templates/mt-console"] = &corepb.MessageTemplate{
			Name: "tenants/t-1/message-templates/mt-console", Key: "tenant-invitation", Channel: "email", Locale: "en",
			// Written as text before kit#953: live, with no published_at.
			Subject:  "Join us",
			HtmlBody: "<p>Hello {{ .Recipient.DisplayName }}, <a href=\"{{ .Invitation.AcceptURL }}\">join us</a>.</p>",
			TextBody: "Hello {{ .Recipient.DisplayName }}, join us: {{ .Invitation.AcceptURL }}",
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
				// Adopted with an import block naming the tenant: the
				// import changes nothing kit holds.
				Config: h.wholeMessage("Join us") + `
import {
  to = authwise_message.invitation
  id = "t-1/tenant-invitation/email/en"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(messageAddr, plancheck.ResourceActionNoop)},
				},
				Check: resource.TestCheckResourceAttr(messageAddr, "name", "tenants/t-1/message-templates/mt-console"),
			},
			{Config: h.wholeMessage("Join us"), PlanOnly: true},
		},
	})
}

// TestAccMessage_Shapes: a message in no shape, two shapes, or a whole mail
// missing a part is refused at plan; a value the message does not offer is
// refused by kit at apply.
func TestAccMessage_Shapes(t *testing.T) {

	h := newHarness(t)
	cfg := func(body string) string {
		return h.providerConfig() + `
resource "authwise_message" "invitation" {
  key    = "tenant-invitation"
  locale = "en"
` + body + "\n}\n"
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{Config: cfg(``), PlanOnly: true, ExpectError: regexp.MustCompile(`a message takes one shape`)},
			{
				Config:      cfg(`texts = { heading = "Hi" }` + "\n" + `subject = "Hi"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not both`),
			},
			{Config: cfg(`subject = "Hi"`), PlanOnly: true, ExpectError: regexp.MustCompile(`the whole mail needs html`)},
			{
				Config:      cfg(`subject = "Hi {{ .Unknown }}"` + "\n" + `html = "<p>Hi</p>"` + "\n" + `text = "Hi"`),
				ExpectError: regexp.MustCompile(`(?s)kit refused the message.*can't evaluate field Unknown`),
			},
			{
				// Named parts wait on kit#953's apis release.
				Config:      cfg(`texts = { heading = "Hi" }`),
				ExpectError: regexp.MustCompile(`kit#953`),
			},
		},
	})
}
