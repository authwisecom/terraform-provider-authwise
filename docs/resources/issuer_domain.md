---
page_title: "authwise_issuer_domain Resource - Authwise"
subcategory: "Identity"
description: |-
  A hostname of yours that serves an issuer, such as login.acme.com. Authwise obtains and renews its certificate; you add two DNS records, read from dns_records. The issuer keeps answering at its own host as well, and each host is its own issuer URL (iss).
  Create returns once the records are known, so DNS resources can take them in the same apply. Add the challenge record first and point traffic only once state is PROVISIONING or ACTIVE, so the certificate exists before any request arrives: authwise_issuer_domain_activation waits for that between the two records, in one apply. Both records must be DNS-only (on Cloudflare, proxied = false), and the challenge record stays for as long as the domain does: renewal depends on it.
  Destroying the domain waits until Authwise has released the hostname, which takes the cloud side's teardown.
---

# authwise_issuer_domain (Resource)

A hostname of yours, such as `login.acme.com`, that serves an issuer.
Authwise obtains the certificate and renews it on its own edge. You add two
DNS records and nothing else.

- **Each host is its own issuer URL.** Tokens minted at `login.acme.com`
  carry `iss = https://login.acme.com`. The issuer's own host keeps
  working, with its own `iss`, so relying parties that cached discovery do
  not break. Neither host redirects to the other.
- **Passkeys do not follow.** A passkey is bound to the host it was
  enrolled at, so passkeys enrolled at the issuer's own host do not work at
  the custom domain. Add the domain before rolling out passkeys, or have
  people enrol again.
- **Business plan or above.** Where an installation enforces the plan,
  create is refused below it with `PLAN_REQUIRED`.

## Example Usage

```terraform
# login.acme.com serves the issuer, with its DNS records on Cloudflare, in
# one apply that follows "challenge first, traffic after PROVISIONING": the
# challenge record, then the activation waiting for the certificate, then
# the traffic record.
resource "authwise_issuer_domain" "login" {
  issuer_id = authwise_issuer.main.issuer_id
  hostname  = "login.acme.com"
}

locals {
  login_records = { for r in authwise_issuer_domain.login.dns_records : r.purpose => r }
}

resource "cloudflare_record" "login_challenge" {
  zone_id = var.zone_id
  name    = local.login_records["challenge"].name
  type    = local.login_records["challenge"].type
  content = local.login_records["challenge"].value

  # DNS-only. Proxied, Cloudflare would stand in front of Authwise, which
  # is not supported, and the certificate authority could not see the
  # challenge record.
  proxied = false
}

resource "authwise_issuer_domain_activation" "login" {
  issuer_domain = authwise_issuer_domain.login.name
  depends_on    = [cloudflare_record.login_challenge]
}

resource "cloudflare_record" "login_traffic" {
  zone_id = var.zone_id
  name    = local.login_records["traffic"].name
  type    = local.login_records["traffic"].type
  content = local.login_records["traffic"].value
  proxied = false

  depends_on = [authwise_issuer_domain_activation.login]
}
```

## The rules

1. **Challenge first, traffic after `PROVISIONING`.** The certificate is
   issued once the certificate authority sees the `challenge` record, before
   any request reaches the custom domain. From `PROVISIONING` the hostname
   routes with its certificate, so if the `traffic` record goes in only then,
   nobody ever sees a TLS error. `ACTIVE` follows once Authwise finds the
   `traffic` record in public DNS. The example does this in one apply:
   `authwise_issuer_domain_activation` sits between the two records and waits
   for `PROVISIONING` or `ACTIVE`. Creating both records at once also
   works; the hostname then fails TLS for the few minutes until the
   certificate exists.
2. **DNS-only records.** On Cloudflare, `proxied = false` (the grey cloud).
   A proxied record puts Cloudflare in front of Authwise. That is not
   supported: the issuer would not see the real host, and the certificate
   authority would not see the challenge record.
3. **Keep the challenge record for as long as the domain exists.** Renewal
   depends on it. If it goes, the domain moves to `DEGRADED` with
   `failure_record = "challenge"`. It keeps serving until the current
   certificate expires. Tenant administrators get mail when the domain
   becomes `DEGRADED`, and again 14 and 7 days before the certificate
   expires if it still is.
4. **A subdomain, not an apex.** A CNAME cannot sit at a zone apex, and
   create refuses one with `HOSTNAME_APEX`.

## Waiting

Create returns once `dns_records` is known, usually within thirty seconds,
so DNS resources can take the records in the same apply. It does not wait
for the certificate: it can't, because the certificate needs the challenge
record, and that record needs the domain.

`authwise_issuer_domain_activation` waits instead. Make it depend on the challenge record, and the traffic record on
it. Its create waits until `state` is `PROVISIONING` or `ACTIVE`, up to
`timeouts.create` (30 minutes by default). It does not wait for `ACTIVE`:
Authwise confirms that by finding the traffic record in public DNS, so it
follows on the controller's next check once that record has propagated.
0.18.0 waited for `ACTIVE`, which never came. A `FAILED` domain ends the wait with `failure_code` and
`failure_message`, and the traffic record is not created.

In 0.17, `wait_for_active = true` on this resource did the waiting. It is
gone from 0.18.0: remove it from the configuration and add the activation
if you used it. State written by 0.17 needs nothing.

Destroying the domain waits until Authwise has removed its certificate and
edge entries and released the hostname, up to `timeouts.delete` (30
minutes). Until then the hostname cannot be claimed again, by a replacement
or by anyone else.

From kit v1.53.1 (kit#796), an issuer with any domain on
it, including one still being deleted, cannot be deleted
(`ISSUER_HAS_DOMAINS`). Set `issuer_id` from the issuer resource, as in the
example, and Terraform destroys the domain, and waits for it to be gone,
before it deletes the issuer. If `issuer_id` is a literal, add `depends_on`
naming the issuer.

## States

| `state` | Meaning | What to do |
|---|---|---|
| `PENDING_DNS` | Waiting for the challenge record. | Add it, DNS-only. A claim never verified lapses, and its hostname is freed after about a week. |
| `VERIFYING` | The certificate authority found the challenge record and is issuing the certificate. | Wait. |
| `PROVISIONING` | The certificate is issued and attached; the hostname routes. | Add the traffic record if you have not; `ACTIVE` follows once it resolves. |
| `ACTIVE` | Serving with its own certificate, and the traffic record resolves. | Nothing. |
| `DEGRADED` | Serving, but renewal will fail. | Restore the record named in `failure_record`. |
| `FAILED` | Issuance stopped. | Fix the cause in `failure_code`, below. |
| `DELETING` | Being removed. | Wait; the hostname is released when it is gone. |

| `failure_code` | Cause |
|---|---|
| `DNS_NOT_FOUND` | The record in `failure_record` does not resolve. |
| `CAA` | A CAA record on the hostname or a parent excludes the certificate authority. Allow `pki.goog` and `letsencrypt.org`, or remove the CAA record. |
| `CONFIG` | A record resolves to something else, or is proxied. |
| `RATE_LIMITED` | The certificate authority's rate limit. Authwise retries. |
| `PROBE_FAILED` | The hostname did not answer as the issuer: the traffic record points elsewhere. |
| `PROVIDER_ERROR` | On Authwise's side. Contact support if it persists. |

Create is refused with an error reason in the diagnostic:
`HOSTNAME_CLAIMED` (another issuer, another tenant, or a domain of yours
still being deleted holds it), `HOSTNAME_APEX`, `HOSTNAME_IS_ISSUER` (the
hostname is already an Authwise issuer host), `PLAN_REQUIRED`, or
`DOMAINS_UNAVAILABLE` (the install has no domain controller, so no domain
on it would get a certificate; from kit v1.53.1, kit#798).

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `hostname` (String) The fully qualified hostname, lower case, without a trailing dot. It must be a subdomain: a CNAME cannot sit at a zone apex. Unique across Authwise; kit refuses one already claimed. Changing it replaces the domain.

### Optional

- `issuer_id` (String) The issuer the hostname serves (`i-…`); overrides the provider default, and reads back from the name. Changing a configured one replaces the domain.
- `tenant_id` (String) Parent identifier `tenant_id`; overrides the provider default, and reads back from the name. Changing a configured one replaces the domain.
- `timeouts` (Block, Optional) (see [below for nested schema](#nestedblock--timeouts))

### Read-Only

- `certificate_expires_at` (String) When the current certificate expires, RFC 3339; empty before the first is issued.
- `dns_records` (Attributes List) The records to add at your DNS provider, one per `purpose`: `traffic` (the hostname, a CNAME to the issuer's own host) and `challenge` (the certificate authority's proof of control). Known after create. Key DNS resources on `purpose`, not on these values, so the keys are known at plan; see the example. (see [below for nested schema](#nestedatt--dns_records))
- `failure_code` (String) Why the domain is `FAILED` or `DEGRADED`; empty otherwise. One of `DNS_NOT_FOUND` (a record is missing), `CAA` (a CAA record excludes the certificate authority), `CONFIG` (a record exists with the wrong content, or is proxied), `RATE_LIMITED` (the certificate authority's limit; it retries), `PROBE_FAILED` (the hostname did not answer as the issuer) or `PROVIDER_ERROR` (Authwise's side).
- `failure_message` (String) `failure_code` in words, with the record or setting to fix.
- `failure_record` (String) The `purpose` of the record the failure is about, `traffic` or `challenge`; empty when it is about neither.
- `issuer_domain_id` (String) Server-assigned resource id — the last segment of `name`.
- `name` (String) Full resource name (`tenants/{t}/issuers/{i}/issuer-domains/{d}`); serves as the Terraform ID.
- `state` (String) `PENDING_DNS` (waiting for the challenge record), `VERIFYING` (issuing the certificate), `PROVISIONING` (attaching it; the hostname routes), `ACTIVE`, `DEGRADED` (serving, but the challenge record is gone and renewal will fail), `FAILED` (see `failure_code`) or `DELETING`.
- `traffic_target` (String) The issuer's own Authwise host, such as `example.id.authwise.com`: the value of the `traffic` record.

<a id="nestedblock--timeouts"></a>
### Nested Schema for `timeouts`

Optional:

- `create` (String) A string that can be [parsed as a duration](https://pkg.go.dev/time#ParseDuration) consisting of numbers and unit suffixes, such as "30s" or "2h45m". Valid time units are "s" (seconds), "m" (minutes), "h" (hours).
- `delete` (String) A string that can be [parsed as a duration](https://pkg.go.dev/time#ParseDuration) consisting of numbers and unit suffixes, such as "30s" or "2h45m". Valid time units are "s" (seconds), "m" (minutes), "h" (hours). Setting a timeout for a Delete operation is only applicable if changes are saved into state before the destroy operation occurs.
- `update` (String) A string that can be [parsed as a duration](https://pkg.go.dev/time#ParseDuration) consisting of numbers and unit suffixes, such as "30s" or "2h45m". Valid time units are "s" (seconds), "m" (minutes), "h" (hours).


<a id="nestedatt--dns_records"></a>
### Nested Schema for `dns_records`

Read-Only:

- `name` (String) The record's name, a fully qualified hostname.
- `purpose` (String) `traffic` or `challenge`.
- `type` (String) The record type, `CNAME`.
- `value` (String) The record's content.

## Import

```shell
# Import by full resource name.
terraform import authwise_issuer_domain.login tenants/t-01/issuers/i-01/issuer-domains/d-01
```
