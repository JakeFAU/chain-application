FROM mcr.microsoft.com/devcontainers/go:2.1.6-1.26-bookworm

ARG GCLOUD_VERSION=579.0.0-0
ARG DBMATE_VERSION=v2.35.0

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ENV CLOUDSDK_PYTHON_SITEPACKAGES=1

RUN apt-get update \
    && export DEBIAN_FRONTEND=noninteractive \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        gnupg \
        postgresql-client \
        ripgrep \
        shellcheck \
    && curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg \
        | gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" \
        > /etc/apt/sources.list.d/google-cloud-sdk.list \
    && apt-get update \
    && CLOUDSDK_SKIP_PY_COMPILATION=1 \
        apt-get install -y --no-install-recommends \
        "google-cloud-cli=${GCLOUD_VERSION}" \
    && rm -rf /var/lib/apt/lists/*

RUN GOBIN=/usr/local/bin \
    go install "github.com/amacneil/dbmate/v2@${DBMATE_VERSION}"

RUN install -d -o vscode -g vscode \
        /home/vscode/.local/bin \
        /home/vscode/.config/gcloud \
        /home/vscode/.config/gh \
        /home/vscode/.codex \
        /home/vscode/go/bin \
        /home/vscode/go/pkg/mod \
        /home/vscode/.cache/go-build \
        /commandhistory