# SAML 2.0, both roles

Authwise speaks SAML in two directions, and they are separate connections
with separate trust:

| Role | Authwise is | Configured on | Data source |
|---|---|---|---|
| SP | the consumer — your users log in through a partner IdP | a `provider` | `authwise_provider_saml` |
| IdP | the issuer — a partner's app logs its users in with Authwise | a `client` | `authwise_saml_relying_party_config` |

`main.tf` sets up one of each against the same fictional partner.

```sh
# Drop the partner's certificates next to main.tf, edit provider.tf
# (endpoint, AWIDs) and the locals at the top of main.tf, then:
terraform init
terraform apply
```

**Requires kit >= 1.20.0** (the release that shipped SAML in both roles) and
provider **>= the release carrying `authwise_certificate`**.

## Certificates are the trust, and they are rows

Every certificate field is a Certificate resource name, never inline PEM on
the connection. Inbound signatures are checked against that one certificate
and nothing else — the `KeyInfo` a partner puts inside an assertion is never
a source of trust — so an anchor has to be a row somebody deliberately
created.

One field decides which lane `authwise_certificate` takes:

- **`import_certificate_pem` set** — import a partner's public certificate.
  `origin` comes back `CERTIFICATE_ORIGIN_IMPORTED` and `has_private_key` is
  false. There is no `ImportCertificate` RPC; the field is the switch.
- **`import_certificate_pem` empty** — mint a key pair. `subject_common_name`,
  `validity_days` and `key_size` steer it and default to `display_name`, 825
  days and RSA-2048.

`certificate_pem` is the read-only public certificate of either kind.

### Mint parameters do not come back

`subject_common_name`, `validity_days`, `key_size` and
`import_certificate_pem` are **input only**: kit consumes them on create and
never returns them, because they describe how to make the material rather
than what was made. `subject`, `not_before` and `not_after` are the answers.

Two consequences worth knowing before you hit them:

- They are create-only, so changing one **replaces the certificate**. That is
  deliberate: swapping a trust anchor under a live connection should be a
  visible act, not a silent in-place edit.
- `terraform import` cannot recover them. An imported certificate has no
  value for them, and the next plan proposes replacement unless your
  configuration repeats what they were. Write them down, or import with
  `-ignore-remote-version` style care and reconcile by hand.

## Metadata import and export are awctl, not Terraform

kit exposes four metadata RPCs — `ImportProviderSamlMetadata`,
`ExportProviderSamlMetadata`, `ImportClientSamlMetadata`,
`ExportClientSamlMetadata`. None of them is a Terraform surface: they are
verbs over a resource, not a resource, and the provider's generator models
Get-by-name data sources and config builders, not arbitrary RPCs.

**Export is read-only and safe to run against Terraform-managed entries.**
It renders the document you hand the partner:

```sh
# The SP metadata for a provider Terraform owns
awctl identity providers export-saml-metadata "$(terraform output -raw provider_name)" \
  --tenant-id t-01 --realm-id r-01 --out sp-metadata.xml

# The IdP metadata for a client Terraform owns
awctl identity clients export-saml-metadata "$(terraform output -raw client_name)" \
  --tenant-id t-01 --issuer-id i-01 --out idp-metadata.xml
```

The entity id and endpoint go to stderr, for a partner who wants those
without the document.

**Import writes, so it fights Terraform.** `import-saml-metadata` fills in
the partner's entity id, endpoint and binding *and creates a Certificate row
per certificate the document publishes* (deduped on fingerprint, so
re-importing is idempotent). Against entries Terraform owns, that is drift:
the next plan reverts it.

Pick one owner per connection:

- **Terraform owns it** (this example): read the partner's metadata yourself
  and write the values into `authwise_provider_saml` / the certificate
  resources. Use `--dry-run` as a reader — it prints the change list and
  writes nothing:

  ```sh
  awctl identity providers import-saml-metadata p-01 \
    --tenant-id t-01 --realm-id r-01 --file idp-metadata.xml --dry-run
  ```

- **awctl owns it**: keep the provider or client out of Terraform entirely.
  A half-managed connection is the bad case.

A principal calling import also needs `identity.certificates.create`, since
import mints Certificate rows. The two import permissions
(`identity.providers.importSamlMetadata`,
`identity.clients.importSamlMetadata`) are separate precisely so granting
metadata import is not a side door into adding a trust anchor — grant them
together with `certificates.create`, as the default catalog does.

## Rotation

`RotateCertificate` (`POST …/certificates/{c}:rotate`) is likewise a verb, not
a Terraform surface. Rotating in Terraform means adding a new
`authwise_certificate` and repointing `signing_certificate_id` at it, which
is an ordinary plan you can review. Use awctl's rotate when you want kit's
overlap handling (the old certificate goes `RETIRING` and keeps verifying
until it is retired) rather than a hard cutover.
