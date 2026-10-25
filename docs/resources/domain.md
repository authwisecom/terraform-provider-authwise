---
page_title: "authwise_domain Resource - Authwise"
subcategory: "Identity"
description: |-
  A hostname the tenant serves logins on. A native host (no issuer_id) is an Authwise host such as acme.id.authwise.com, which an issuer names as its host (authwise_issuer.domain_id). A custom host (issuer_id set) is a hostname of yours that serves that issuer, such as login.acme.com: Authwise obtains and renews its certificate, and you add two DNS records, read from dns_records. The issuer keeps answering at its own host as well, and each host is its own issuer URL (iss).
  For a custom host, create returns once the records are known, so DNS resources can take them in the same apply. Add the challenge record first and point traffic only once state is PROVISIONING or ACTIVE, so the certificate exists before any request arrives: authwise_domain_activation waits for that between the two records, in one apply. Both records must be DNS-only (on Cloudflare, proxied = false), and the challenge record stays for as long as the domain does: renewal depends on it. Destroying a custom host waits until Authwise has released the hostname, which takes the cloud side's teardown.
---

# authwise_domain (Resource)

A hostname the tenant serves logins on. There are two kinds, told apart by
`issuer_id`:

- **Native** (no `issuer_id`): an Authwise host, such as
  `acme.id.authwise.com`. An issuer names it as its host
  (`authwise_issuer.domain_id`), so it exists before the issuer does.
- **Custom** (`issuer_id` set): a hostname of yours, such as
  `login.acme.com`, that serves that issuer. Authwise obtains the
  certificate and renews it on its own edge. You add two DNS records and
  nothing else.

`domain_id` is the domain's id (`d-…`), assigned by Authwise. Before 0.19.0
it was the hostname itself; see [Upgrading from 0.18](#upgrading-from-018).

About custom hosts:

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
# A native host: the issuer's own Authwise hostname.
resource "authwise_domain" "main" {
  hostname = "acme.id.authwise.com"
}

resource "authwise_issuer" "main" {
  domain_id = authwise_domain.main.domain_id
}

# A custom host: login.acme.com serves the issuer, with its DNS records on
# Cloudflare, in one apply that follows "challenge first, traffic after
# PROVISIONING": the challenge record, then the activation waiting for the
# certificate, then the traffic record.
resource "authwise_domain" "login" {
  issuer_id = authwise_issuer.main.issuer_id
  hostname  = "login.acme.com"
}

locals {
  login_records = { for r in authwise_domain.login.dns_records : r.purpose => r }
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

resource "authwise_domain_activation" "login" {
  domain     = authwise_domain.login.name
  depends_on = [cloudflare_record.login_challenge]
}

resource "cloudflare_record" "login_traffic" {
  zone_id = var.zone_id
  name    = local.login_records["traffic"].name
  type    = local.login_records["traffic"].type
  content = local.login_records["traffic"].value
  proxied = false

  depends_on = [authwise_domain_activation.login]
}
```

## The rules for a custom host

1. **Challenge first, traffic after `PROVISIONING`.** The certificate is
   issued once the certificate authority sees the `challenge` record, before
   any request reaches the custom domain. From `PROVISIONING` the hostname
   routes with its certificate, so if the `traffic` record goes in only then,
   nobody ever sees a TLS error. `ACTIVE` follows once Authwise finds the
   `traffic` record in public DNS. The example does this in one apply:
   `authwise_domain_activation` sits between the two records and waits for
   `PROVISIONING` or `ACTIVE`. Creating both records at once also works;
   the hostname then fails TLS for the few minutes until the certificate
   exists.
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

For a custom host, create returns once `dns_records` is known, usually
within thirty seconds, so DNS resources can take the records in the same
apply. It does not wait for the certificate: it can't, because the
certificate needs the challenge record, and that record needs the domain.

`authwise_domain_activation` waits instead. Make it depend on the challenge
record, and the traffic record on it. Its create waits until `state` is
`PROVISIONING` or `ACTIVE`, up to `timeouts.create` (30 minutes by
default). A `FAILED` domain ends the wait with `failure_code` and
`failure_message`, and the traffic record is not created.

Destroying a custom host waits until Authwise has removed its certificate
and edge entries and released the hostname, up to `timeouts.delete` (30
minutes). Until then the hostname cannot be claimed again, by a replacement
or by anyone else. A native host is deleted at once, and is refused while
an issuer names it (`DOMAIN_HAS_ISSUERS`).

An issuer with a custom host on it, including one still being deleted,
cannot be deleted (`ISSUER_HAS_DOMAINS`). Set `issuer_id` from the issuer
resource, as in the example, and Terraform destroys the domain, and waits
for it to be gone, before it deletes the issuer. If `issuer_id` is a
literal, add `depends_on` naming the issuer.

## States

A native host has no state: `state` is empty and `dns_records` is empty.

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
`HOSTNAME_CLAIMED` (another issuer, another tenant, or a domain still being
deleted holds it), `HOSTNAME_APEX`, `HOSTNAME_IS_ISSUER` (the hostname is
already an Authwise issuer host), `PLAN_REQUIRED`, or `DOMAINS_UNAVAILABLE`
(the install has no domain controller, so no custom host on it would get a
certificate).

## Upgrading from 0.18

0.19.0 needs [kit v1.56.0](https://gitlab.authwise.io/authwise/kit/-/releases/v1.56.0) or later (its API is [apis v0.40.0](https://git.authwise.com/authwise/apis/-/releases/v0.40.0)), the release that gave every domain an id
(kit#808). Upgrade kit first. Before the first apply with 0.19.0, keep a
copy of the state (`terraform state pull > before-0.19.tfstate`): once
0.19.0 has written it, 0.18 cannot read it.

### `authwise_domain`

In 0.18, `domain_id` was the hostname, and an issuer named its host by
that hostname in `domain_name`. Change the configuration:

```terraform
# 0.18
resource "authwise_domain" "main" {
  domain_id = "acme.id.authwise.com"
}

resource "authwise_issuer" "main" {
  domain_name = authwise_domain.main.domain_id
}

# 0.19
resource "authwise_domain" "main" {
  hostname = "acme.id.authwise.com"
}

resource "authwise_issuer" "main" {
  domain_id = authwise_domain.main.domain_id
}
```

Then run `terraform plan`. The state upgrades itself: the refresh finds
each domain by its hostname and records its id, and the issuer reads the
same id back from Authwise. **The plan should show no changes.** If it
plans to replace an `authwise_issuer`, do not apply: kit is older than
v1.56.0, or has not finished giving the issuer its domain's id. Check the
kit version and plan again.

`config` is gone from the resource: it had no settings. Remove it from the
configuration if it is there.

If the state cannot be upgraded in place, re-import instead, by id or by
hostname:

```shell
terraform state rm authwise_domain.main
terraform import authwise_domain.main tenants/t-01/domains/acme.id.authwise.com
```

### `authwise_issuer_domain` and `authwise_issuer_domain_activation`

Both are deprecated: a custom host is an `authwise_domain` with
`issuer_id`, and the same row in Authwise. Rename the resources in the
configuration, `issuer_domain` to `domain` in the activation, and add
`moved` blocks (Terraform 1.8 or later):

```terraform
resource "authwise_domain" "login" {
  issuer_id = authwise_issuer.main.issuer_id
  hostname  = "login.acme.com"
}

resource "authwise_domain_activation" "login" {
  domain     = authwise_domain.login.name
  depends_on = [cloudflare_record.login_challenge]
}

moved {
  from = authwise_issuer_domain.login
  to   = authwise_domain.login
}

moved {
  from = authwise_issuer_domain_activation.login
  to   = authwise_domain_activation.login
}
```

The plan should show both as moved with no changes, and the DNS records
unchanged: the domain keeps its id and its certificate. **Never let
Terraform destroy and recreate a custom host to migrate it.** That deletes
its certificate and edge entries and releases the hostname, and logins at
it fail until a new certificate is issued.

On Terraform older than 1.8, re-import instead:

```shell
terraform state rm authwise_issuer_domain.login authwise_issuer_domain_activation.login
terraform import authwise_domain.login tenants/t-01/domains/d-01
terraform import authwise_domain_activation.login tenants/t-01/domains/d-01
```

`d-01` is the old resource's `issuer_domain_id`: the id did not change.
The deprecated resources go once kit removes the API they call (kit#810).

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `hostname` (String) The hostname, lower case, without a scheme or trailing dot. Unique across Authwise; kit refuses one already claimed. Changing it replaces the domain. A custom host must be a fully qualified subdomain, since a CNAME cannot sit at a zone apex; a native one may carry a port.

### Optional

- `issuer_id` (String) The issuer a custom host serves (`i-…`); unset for a native host. Changing it replaces the domain. Set it from the issuer resource, so Terraform destroys the domain before the issuer: kit refuses to delete an issuer with a domain on it (`ISSUER_HAS_DOMAINS`).
- `labels` (Map of String)
- `tenant_id` (String) Parent identifier `tenant_id`; overrides the provider default, and reads back from the name. Changing a configured one replaces the domain.
- `timeouts` (Block, Optional) (see [below for nested schema](#nestedblock--timeouts))

### Read-Only

- `activated_at` (String) When a custom host first became `ACTIVE`, RFC 3339; empty before then.
- `certificate_expires_at` (String) When a custom host's current certificate expires, RFC 3339; empty before the first is issued.
- `dns_records` (Attributes List) A custom host's records to add at your DNS provider, one per `purpose`: `traffic` (the hostname, a CNAME to the issuer's own host) and `challenge` (the certificate authority's proof of control). Known after create; empty on a native host. Key DNS resources on `purpose`, not on these values, so the keys are known at plan; see the example. (see [below for nested schema](#nestedatt--dns_records))
- `domain_id` (String) Server-assigned resource id (`d-…`), the last segment of `name`: what `authwise_issuer.domain_id` takes.
- `failure_code` (String) Why the domain is `FAILED` or `DEGRADED`; empty otherwise. One of `DNS_NOT_FOUND` (a record is missing), `CAA` (a CAA record excludes the certificate authority), `CONFIG` (a record exists with the wrong content, or is proxied), `RATE_LIMITED` (the certificate authority's limit; it retries), `PROBE_FAILED` (the hostname did not answer as the issuer) or `PROVIDER_ERROR` (Authwise's side).
- `failure_message` (String) `failure_code` in words, with the record or setting to fix.
- `failure_record` (String) The `purpose` of the record the failure is about, `traffic` or `challenge`; empty when it is about neither.
- `name` (String) Full resource name (`tenants/{t}/domains/{d}`); serves as the Terraform ID.
- `state` (String) A custom host's state, empty on a native host: `PENDING_DNS` (waiting for the challenge record), `VERIFYING` (issuing the certificate), `PROVISIONING` (attaching it; the hostname routes), `ACTIVE`, `DEGRADED` (serving, but the challenge record is gone and renewal will fail), `FAILED` (see `failure_code`) or `DELETING`.
- `traffic_target` (String) A custom host's issuer's own Authwise host, such as `example.id.authwise.com`: the value of the `traffic` record.

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
terraform import authwise_domain.login tenants/t-01/domains/d-01

# Or by the hostname in place of the id, as state written by 0.18 and
# earlier named it; the import resolves it to the domain's id.
terraform import authwise_domain.main tenants/t-01/domains/acme.id.authwise.com
```
