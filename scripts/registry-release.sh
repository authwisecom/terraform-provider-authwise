#!/usr/bin/env bash
# Package, sign and publish a release in the shape the Terraform Registry
# ingests: a GitHub Release on authwisecom/terraform-provider-authwise carrying
#
#   terraform-provider-authwise_<ver>_<os>_<arch>.zip   (one per platform)
#   terraform-provider-authwise_<ver>_manifest.json
#   terraform-provider-authwise_<ver>_SHA256SUMS        (covers the above)
#   terraform-provider-authwise_<ver>_SHA256SUMS.sig    (binary detached sig)
#
# Input is the raw stamped binaries the .dist CI block leaves in dist/.
#
# Env:
#   VERSION          X.Y.Z (required; the version job's dotenv in CI)
#   GPG_FINGERPRINT  signing key (required)
#   GPG_PRIVATE_KEY  path to an armored secret key (CI File variable); when
#                    unset the ambient keyring is used — handy for local runs
#   GPG_PASSPHRASE   passphrase for that key, if it has one
#   GITHUB_TOKEN     fine-grained PAT, Contents read/write on the repo
#   GITHUB_REPOSITORY  owner/repo (default authwisecom/terraform-provider-authwise)
#   DRY_RUN=1        package and sign only; touch nothing on GitHub
set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${GPG_FINGERPRINT:?GPG_FINGERPRINT is required}"

NAME=terraform-provider-authwise
TAG="v${VERSION}"
REPO="${GITHUB_REPOSITORY:-authwisecom/terraform-provider-authwise}"
OUT="${PWD}/dist/registry"
SUMS="${NAME}_${VERSION}_SHA256SUMS"

# --- package -----------------------------------------------------------------
# Each zip holds exactly one file, the binary renamed to the registry's
# terraform-provider-<name>_v<ver>[.exe] convention.
rm -rf "$OUT"
mkdir -p "$OUT"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
found=0
for bin in dist/"${NAME}_${VERSION}"_*; do
  base=$(basename "$bin")
  case "$base" in *SHA256SUMS*) continue ;; esac
  ext=""
  [ "${base%.exe}" != "$base" ] && ext=".exe"
  platform=${base#"${NAME}_${VERSION}_"}
  platform=${platform%.exe}
  cp "$bin" "${stage}/${NAME}_v${VERSION}${ext}"
  chmod 0755 "${stage}/${NAME}_v${VERSION}${ext}"
  zip -q -j -X "${OUT}/${NAME}_${VERSION}_${platform}.zip" "${stage}/${NAME}_v${VERSION}${ext}"
  rm "${stage}/${NAME}_v${VERSION}${ext}"
  found=$((found + 1))
done
[ "$found" -gt 0 ] || { echo "ERROR: no ${NAME}_${VERSION}_* binaries in dist/" >&2; exit 1; }

cp terraform-registry-manifest.json "${OUT}/${NAME}_${VERSION}_manifest.json"
(cd "$OUT" && shasum -a 256 ./*.zip ./*_manifest.json | sed 's| \./| |' > "$SUMS")

# --- sign --------------------------------------------------------------------
# CI imports the key into a throwaway keyring so nothing outlives the job.
if [ -n "${GPG_PRIVATE_KEY:-}" ]; then
  GNUPGHOME=$(mktemp -d)
  export GNUPGHOME
  chmod 700 "$GNUPGHOME"
  trap 'rm -rf "$stage" "$GNUPGHOME"' EXIT
  gpg --batch --quiet --import "$GPG_PRIVATE_KEY"
fi
sign=(gpg --batch --yes --local-user "$GPG_FINGERPRINT" --detach-sign --output "${OUT}/${SUMS}.sig")
if [ -n "${GPG_PASSPHRASE:-}" ]; then
  printf '%s' "$GPG_PASSPHRASE" | "${sign[@]}" --pinentry-mode loopback --passphrase-fd 0 "${OUT}/${SUMS}"
else
  "${sign[@]}" "${OUT}/${SUMS}"
fi
gpg --batch --verify "${OUT}/${SUMS}.sig" "${OUT}/${SUMS}"
(cd "$OUT" && shasum -a 256 -c "$SUMS")

if [ "${DRY_RUN:-}" = "1" ]; then
  echo "DRY_RUN: packaged and signed ${TAG} in ${OUT}; GitHub untouched"
  ls -l "$OUT"
  exit 0
fi

# --- publish -----------------------------------------------------------------
: "${GITHUB_TOKEN:?GITHUB_TOKEN is required unless DRY_RUN=1}"
api() {
  curl -fsS \
    -H "Authorization: Bearer ${GITHUB_TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "$@"
}

# Push the annotated tag (and the history it needs) ourselves rather than
# waiting on the GitLab push mirror; if the mirror got there first this is
# a no-op, and if GitHub holds a different tag of the same name it fails.
git push "https://git:${GITHUB_TOKEN}@github.com/${REPO}.git" "refs/tags/${TAG}:refs/tags/${TAG}"

# A published release is final (the registry has likely ingested it). A
# draft is debris from a failed earlier attempt: clear it and start over.
existing=$(api "https://api.github.com/repos/${REPO}/releases?per_page=100" |
  jq -r --arg t "$TAG" '.[] | select(.tag_name == $t) | "\(.id) \(.draft)"')
while read -r id draft; do
  [ -n "$id" ] || continue
  if [ "$draft" != "true" ]; then
    echo "ERROR: ${TAG} is already published on github.com/${REPO}" >&2
    exit 1
  fi
  api -X DELETE "https://api.github.com/repos/${REPO}/releases/${id}"
done <<< "$existing"

# Create as a draft, attach everything, then publish, so the registry's
# webhook only ever sees a complete release.
notes=$(git tag -l --format='%(contents)' "$TAG")
id=$(api -X POST "https://api.github.com/repos/${REPO}/releases" \
  -d "$(jq -n --arg t "$TAG" --arg b "$notes" '{tag_name: $t, name: $t, body: $b, draft: true}')" |
  jq -r .id)
for f in "$OUT"/*; do
  api -X POST -H "Content-Type: application/octet-stream" --data-binary "@${f}" \
    "https://uploads.github.com/repos/${REPO}/releases/${id}/assets?name=$(basename "$f")" >/dev/null
  echo "attached $(basename "$f")"
done
api -X PATCH "https://api.github.com/repos/${REPO}/releases/${id}" -d '{"draft": false}' >/dev/null
echo "published https://github.com/${REPO}/releases/tag/${TAG}"
