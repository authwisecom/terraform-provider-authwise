---
page_title: "authwise_issuer_domain Resource - Authwise"
subcategory: "Identity"
description: |-
  A hostname of yours that serves an issuer, such as login.acme.com. Authwise obtains and renews its certificate; you add two DNS records, read from dns_records. The issuer keeps answering at its own host as well, and each host is its own issuer URL (iss).
  Create returns once the records are known, so DNS resources can take them in the same apply. Add the challenge record first and point traffic only once state is ACTIVE; the certificate then exists before any request arrives. Both records must be DNS-only (on Cloudflare, proxied = false), and the challenge record stays for as long as the domain does: renewal depends on it.
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
# login.acme.com serves the issuer, with the DNS records on Cloudflare in
# the same apply. The records are keyed on purpose, which is known at plan;
# keying them on dns_records' own values would fail, because those are
# known only after the domain is created.
resource "authwise_issuer_domain" "login" {
  issuer_id = authwise_issuer.main.issuer_id
  hostname  = "login.acme.com"
}

resource "cloudflare_record" "authwise" {
  for_each = toset(["traffic", "challenge"])

  zone_id = var.zone_id
  name    = one([for r in authwise_issuer_domain.login.dns_records : r.name if r.purpose == each.key])
  type    = one([for r in authwise_issuer_domain.login.dns_records : r.type if r.purpose == each.key])
  content = one([for r in authwise_issuer_domain.login.dns_records : r.value if r.purpose == each.key])

  # DNS-only. Proxied, Cloudflare would stand in front of Authwise, which
  # is not supported, and the certificate authority could not see the
  # challenge record.
  proxied = false
}
```

## The rules

1. **Challenge first, traffic after `ACTIVE`.** The certificate is issued
   once the certificate authority sees the `challenge` record, before any
   request reaches the custom domain. If the `traffic` record goes in only
   when `state` is `ACTIVE`, nobody ever sees a TLS error. Creating both at
   once, as the example does, works too; the hostname then fails TLS for
   the few minutes until the certificate exists.
2. **DNS-only records.** On Cloudflare, `proxied = false` (the grey cloud).
   A proxied record puts Cloudflare in front of Authwise. That is not
   supported: the issuer would not see the real host, and the certificate
   authority would not see the challenge record.
3. **Keep the challenge record for as long as the domain exists.** Renewal
   depends on it. If it goes, the domain moves to `DEGRADED` with
   `failure_record = "challenge"`. It keeps serving until the current
   certificate expires, and tenant administrators get mail at 30, 14 and 7
   days before that.
4. **A subdomain, not an apex.** A CNAME cannot sit at a zone apex, and
   create refuses one with `HOSTNAME_APEX`.

## Waiting

Create returns once `dns_records` is known, usually within thirty seconds,
so DNS resources can take the records in the same apply. It does not wait
for the certificate unless `wait_for_active = true`. Then create, or the
update that sets it, waits until `state` is `ACTIVE`, up to
`timeouts.create` (or `timeouts.update`), 30 minutes by default. Set it only
when the challenge record is already in place or created without waiting on
this resource; otherwise the wait cannot finish within the same apply.

A `FAILED` domain ends the wait with `failure_code` and `failure_message`.

Destroying the domain waits until Authwise has removed its certificate and
edge entries and released the hostname, up to `timeouts.delete` (30
minutes). Until then the hostname cannot be claimed again, by a replacement
or by anyone else.

From the kit release after v1.53.0 (kit#796), an issuer with any domain on
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
| `PROVISIONING` | The certificate is issued and being attached to the hostname. | Wait. |
| `ACTIVE` | Serving with its own certificate. | Add the traffic record if you have not. |
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
on it would get a certificate; from the kit release after v1.53.0,
kit#798).

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `hostname` (String) The fully qualified hostname, lower case, without a trailing dot. It must be a subdomain: a CNAME cannot sit at a zone apex. Unique across Authwise; kit refuses one already claimed. Changing it replaces the domain.

### Optional

- `issuer_id` (String) The issuer the hostname serves (`i-…`); overrides the provider default, and reads back from the name. Changing a configured one replaces the domain.
- `tenant_id` (String) Parent identifier `tenant_id`; overrides the provider default, and reads back from the name. Changing a configured one replaces the domain.
- `timeouts` (Block, Optional) (see [below for nested schema](#nestedblock--timeouts))
- `wait_for_active` (Boolean) When `true`, create (and an update that sets it) waits until `state` is `ACTIVE`, up to the `create` (or `update`) timeout, default 30 minutes. The certificate is issued only after the challenge record resolves, so set it only where that record already exists or is made outside this apply's dependency on the domain. Default `false`.

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

`wait_for_active` and `timeouts` are configuration only, so an import reads
them as their defaults. When the configuration sets `wait_for_active =
true`, the first plan after an import shows it as an in-place update, which
only reads the domain back.
