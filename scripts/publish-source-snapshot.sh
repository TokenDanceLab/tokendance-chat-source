#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

revision="${1:-HEAD}"
source_sha="$(git rev-parse --verify "${revision}^{commit}")"
source_tag="source-${source_sha}"
source_remote="${CHAT_SOURCE_REMOTE:-https://github.com/TokenDanceLab/tokendance-chat-source.git}"

for required in LICENSE NOTICE LICENSES backend frontend; do
  git cat-file -e "${source_sha}:${required}"
done

temp_root="$(mktemp -d)"
trap 'rm -rf "$temp_root"' EXIT
snapshot="$temp_root/source"
mkdir -p "$snapshot"

# Publish only the product source and build inputs. Operational deployment
# material (nginx, host-specific service units, deploy scripts, internal agent
# handoff notes) is deliberately not part of the public snapshot.
source_paths=(
  .dockerignore
  .editorconfig
  .env.example
  .env.local.example
  .gitattributes
  .gitignore
  AGENTS.md
  CHANGELOG.md
  CONTRIBUTING.md
  DESIGN.md
  Dockerfile
  Dockerfile.runtime
  LICENSE
  LICENSES
  NOTICE
  README.md
  RELEASE.md
  ROADMAP.md
  SECURITY.md
  backend
  docker-compose.yml
  docs/agenthub-validation.md
  docs/bot-architecture.md
  docs/capability-matrix.md
  docs/engineering-goal.md
  docs/llm-api-reference.md
  docs/oidc-setup.md
  docs/security-architecture.md
  docs/security-risk-register.md
  docs/source-boundary.md
  docs/visual-acceptance.md
  frontend
  scripts/build.sh
  scripts/publish-source-snapshot.sh
  scripts/verify-account-bff.py
)
git archive --format=tar "$source_sha" -- "${source_paths[@]}" | tar -xf - -C "$snapshot"

if rg -q \
  -e 'BEGIN[[:space:]]+((RSA|OPENSSH|EC|DSA)[[:space:]]+)?PRIVATE[[:space:]]+KEY-----' \
  -e 'AKIA[0-9A-Z]{16}' \
  -e 'gh[pousr]_[A-Za-z0-9]{20,}' \
  -e 'github_pat_[A-Za-z0-9_]{20,}' \
  -e 'sk-[A-Za-z0-9]{20,}' \
  "$snapshot"; then
  echo "refusing to publish source snapshot: credential-shaped content found" >&2
  exit 1
fi

git -C "$snapshot" init --quiet
git -C "$snapshot" config user.name "TokenDance Source Publisher"
git -C "$snapshot" config user.email "source-publisher@tokendance.invalid"
git -C "$snapshot" add -A

source_date="$(git -C "$repo_root" show -s --format=%aI "$source_sha")"
GIT_AUTHOR_DATE="$source_date" \
GIT_COMMITTER_DATE="$source_date" \
git -C "$snapshot" commit --quiet -m "source: ${source_sha}"
git -C "$snapshot" tag "$source_tag"

git -C "$snapshot" remote add origin "$source_remote"
# Source releases are immutable tags. Never rewrite the public default branch.
remote_tag_sha="$(
  git -C "$snapshot" ls-remote --tags origin "refs/tags/${source_tag}" 2>/dev/null |
    awk -v ref="refs/tags/${source_tag}" '$2 == ref { print $1; exit }'
)"
local_tag_sha="$(git -C "$snapshot" rev-parse HEAD)"
if [ -z "$remote_tag_sha" ]; then
  git -C "$snapshot" push origin "refs/tags/${source_tag}"
elif [ "$remote_tag_sha" != "$local_tag_sha" ]; then
  echo "refusing to replace existing source tag ${source_tag}: remote=${remote_tag_sha} local=${local_tag_sha}" >&2
  exit 1
else
  echo "Source tag ${source_tag} already points at the published snapshot"
fi

echo "Published ${source_sha}"
echo "https://github.com/TokenDanceLab/tokendance-chat-source/tree/${source_tag}"
