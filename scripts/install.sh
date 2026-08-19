#!/bin/sh
# Install a published build of terraform-provider-authwise from the GitLab
# package registry into Terraform's implied local mirror
# (~/.terraform.d/plugins), so configurations can use it before the
# provider is on a real Terraform registry:
#
#   required_providers {
#     authwise = {
#       source  = "authwisecom/authwise"
#       version = "= <version>"     # dev builds are prereleases: pin exactly
#     }
#   }
#
# Usage:
#   GITLAB_TOKEN=<token with read_api> ./scripts/install.sh [version]
#
# Without a version argument the newest published package is installed.
set -eu

API="${AUTHWISE_GITLAB_API:-https://git.authwise.com/api/v4/projects/7}"
TOKEN="${GITLAB_TOKEN:?set GITLAB_TOKEN to a GitLab token with read_api}"

VERSION="${1:-}"
if [ -z "${VERSION}" ]; then
  VERSION=$(curl -fsS -H "PRIVATE-TOKEN: ${TOKEN}" \
    "${API}/packages?package_type=generic&package_name=terraform-provider-authwise&order_by=created_at&sort=desc&per_page=1" |
    sed -n 's/.*"version":"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "${VERSION}" ] || { echo "no published package found; pass a version explicitly" >&2; exit 1; }
fi

case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux) OS=linux ;;
  *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64 | aarch64) ARCH=arm64 ;;
  x86_64) ARCH=amd64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

DEST="${HOME}/.terraform.d/plugins/registry.terraform.io/authwisecom/authwise/${VERSION}/${OS}_${ARCH}"
mkdir -p "${DEST}"

curl -fSL -H "PRIVATE-TOKEN: ${TOKEN}" \
  -o "${DEST}/terraform-provider-authwise_v${VERSION}" \
  "${API}/packages/generic/terraform-provider-authwise/${VERSION}/terraform-provider-authwise_${VERSION}_${OS}_${ARCH}"
chmod +x "${DEST}/terraform-provider-authwise_v${VERSION}"

cat <<EOF
Installed terraform-provider-authwise ${VERSION} (${OS}_${ARCH}) into
${DEST}

Use it with:

  terraform {
    required_providers {
      authwise = {
        source  = "authwisecom/authwise"
        version = "= ${VERSION}"
      }
    }
  }
EOF

# An explicit provider_installation block in the CLI config disables the
# implied local mirror this script installs into.
if grep -qs "provider_installation" "${HOME}/.terraformrc"; then
  cat <<EOF

NOTE: ~/.terraformrc has an explicit provider_installation block, which
disables Terraform's implied local mirror. For this provider to resolve,
the block must include:

  filesystem_mirror {
    path    = "${HOME}/.terraform.d/plugins"
    include = ["registry.terraform.io/authwisecom/*"]
  }
  direct {
    exclude = ["registry.terraform.io/authwisecom/*"]
  }
EOF
fi
