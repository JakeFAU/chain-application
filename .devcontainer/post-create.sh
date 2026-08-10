#!/usr/bin/env bash
set -euo pipefail

readonly REPO_ROOT="$(
  cd "$(dirname "${BASH_SOURCE[0]}")/.."
  pwd
)"
readonly TOOL_VERSIONS_FILE="${REPO_ROOT}/.devcontainer/tool-versions.env"

if [[ ! -f "${TOOL_VERSIONS_FILE}" ]]; then
  echo "missing tool version file: ${TOOL_VERSIONS_FILE}" >&2
  exit 1
fi

# shellcheck source=tool-versions.env
source "${TOOL_VERSIONS_FILE}"

export GOBIN="/home/vscode/go/bin"
export PATH="/home/vscode/.local/bin:${GOBIN}:${PATH}"
export CLOUDSDK_CONFIG="/home/vscode/.config/gcloud"

readonly CONTAINER_USER="$(id -un)"
readonly CONTAINER_GROUP="$(id -gn)"

sudo install -d -o "${CONTAINER_USER}" -g "${CONTAINER_GROUP}" \
  /home/vscode/.local \
  /home/vscode/.local/bin \
  /home/vscode/.config \
  /home/vscode/.config/gcloud \
  /home/vscode/.config/gh \
  /home/vscode/.codex \
  /home/vscode/go \
  /home/vscode/go/bin \
  /home/vscode/go/pkg/mod \
  /home/vscode/.cache/go-build \
  /commandhistory

sudo chown -R "${CONTAINER_USER}:${CONTAINER_GROUP}" \
  /home/vscode/.local \
  /home/vscode/.config/gcloud \
  /home/vscode/.config/gh \
  /home/vscode/.codex \
  /home/vscode/go \
  /home/vscode/.cache/go-build \
  /commandhistory

chmod 700 \
  /home/vscode/.codex \
  /home/vscode/.config/gcloud \
  /home/vscode/.config/gh

cd "${REPO_ROOT}"

install_go_tool() {
  local package="$1"
  local version="$2"

  echo "Installing ${package}@${version}"
  GOBIN="${GOBIN}" go install "${package}@${version}"
}

echo "Downloading Go module dependencies..."
go mod download

echo "Installing repository-pinned tools..."
make tools

echo "Installing pinned interactive Go tools..."
install_go_tool "golang.org/x/tools/gopls" "${GOPLS_VERSION}"
install_go_tool "github.com/go-delve/delve/cmd/dlv" "${DELVE_VERSION}"
install_go_tool "golang.org/x/tools/cmd/goimports" "${GOIMPORTS_VERSION}"
install_go_tool "gotest.tools/gotestsum" "${GOTESTSUM_VERSION}"

echo "Installing Codex CLI ${CODEX_VERSION}..."
npm install \
  --global \
  --prefix /home/vscode/.local \
  --no-audit \
  --no-fund \
  "@openai/codex@${CODEX_VERSION}"

echo
echo "Installed toolchain:"
go version
gopls version
dlv version | head -n 1
staticcheck -version
govulncheck --version
dbmate --version
gcloud version | head -n 1
gh --version | head -n 1
docker version --format 'Docker client {{.Client.Version}}; server {{.Server.Version}}'
codex --version

echo
if ! codex login status >/dev/null 2>&1; then
  echo "Codex is not authenticated."
  echo "Run: codex login --device-auth"
fi

if ! gh auth status >/dev/null 2>&1; then
  echo "GitHub CLI is not authenticated."
  echo "Run: gh auth login"
fi

if [[ -z "$(gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null)" ]]; then
  echo "Google Cloud CLI is not authenticated."
  echo "Run: gcloud auth login --no-launch-browser"
fi

if ! gcloud auth application-default print-access-token >/dev/null 2>&1; then
  echo "Google Application Default Credentials are not configured."
  echo "Run: gcloud auth application-default login --no-launch-browser"
fi

echo
echo "Devcontainer setup complete."