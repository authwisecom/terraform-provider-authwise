package acctest_test

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	corepb "git.authwise.com/authwise/apis/authwise/types/core/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The realm config blocks kit v1.36.0 admits on write (apis v0.19.0 and
// v0.20.0): recovery, bot_protection and sms. The rules are kit's
// realm_admission.go checkRecoveryPolicy and botcheck.Validate.

var regionCode = regexp.MustCompile(`^[A-Z]{2}$`)

// admitRealmConfigLocked refuses the realm configs kit refuses, and any
// secret reference that names no secret.
func (f *fakeIdentityServer) admitRealmConfigLocked(r *corepb.Realm) error {

	if err := f.admitLocked(r); err != nil {
		return err
	}

	if ttl := r.GetConfig().GetRecovery().GetResetTtl(); ttl != nil {
		if d := ttl.AsDuration(); d < 5*time.Minute || d > 24*time.Hour {
			return status.Errorf(codes.InvalidArgument,
				"recovery policy sets reset_ttl %s, outside the 5m0s–24h0m0s a reset link may live; omit the field for the 1h0m0s default", d)
		}
	}

	var problems []string

	if bp := r.GetConfig().GetBotProtection(); bp != nil {
		switch bp.GetMode() {
		case corepb.BotProtection_ADAPTIVE:
			problems = append(problems, "bot_protection.mode ADAPTIVE is not implemented yet; use ALWAYS or OFF")
		case corepb.BotProtection_ALWAYS:
			switch bp.GetProvider() {
			case corepb.BotProtection_TURNSTILE, corepb.BotProtection_HCAPTCHA:
				if bp.GetSiteKey() == "" {
					problems = append(problems, "bot_protection.site_key is required for "+bp.GetProvider().String())
				}
				if bp.GetSecretRef().GetName() == "" {
					problems = append(problems, "bot_protection.secret_ref is required for "+bp.GetProvider().String())
				}
			case corepb.BotProtection_ENDPOINT:
				if bp.GetEndpointName() == "" {
					problems = append(problems, "bot_protection.endpoint_name is required for provider ENDPOINT")
				}
			default:
				problems = append(problems, "bot_protection.mode ALWAYS needs a provider: TURNSTILE, HCAPTCHA or ENDPOINT")
			}
		case corepb.BotProtection_OFF:
			// Off admits any provider settings, kept for turning it on.
		}
		if t := bp.GetTimeout(); t != nil && t.AsDuration() <= 0 {
			problems = append(problems, "bot_protection.timeout must be positive; omit it for the 3s default")
		}
	}

	for name, regions := range map[string][]string{
		"allowed_regions": r.GetConfig().GetSms().GetAllowedRegions(),
		"denied_regions":  r.GetConfig().GetSms().GetDeniedRegions(),
	} {
		for _, region := range regions {
			if !regionCode.MatchString(region) {
				problems = append(problems, fmt.Sprintf("sms.%s %q is not an ISO 3166-1 alpha-2 code (two upper-case letters)", name, region))
			}
		}
	}

	if len(problems) > 0 {
		return status.Errorf(codes.InvalidArgument, "realm config sets %s.", strings.Join(problems, "; and "))
	}

	return nil
}
