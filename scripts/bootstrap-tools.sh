#!/usr/bin/env bash
set -euo pipefail
# Run in the development container or a Linux/macOS shell with Go 1.27.4.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m); case "$arch" in aarch64|arm64) arch=arm64;; x86_64) arch=amd64;; *) exit 1;; esac
dest=${TOOL_BIN:-/usr/local/bin}
mkdir -p "$dest"
curl -fsSL "https://github.com/kubernetes-sigs/kind/releases/download/v0.33.0/kind-${os}-${arch}" -o "$dest/kind"
curl -fsSL "https://dl.k8s.io/release/v1.37.1/bin/${os}/${arch}/kubectl" -o "$dest/kubectl"
curl -fsSL "https://get.helm.sh/helm-v4.3.1-${os}-${arch}.tar.gz" -o /tmp/scalepilot-helm.tgz
tar -xzf /tmp/scalepilot-helm.tgz -C /tmp
cp "/tmp/${os}-${arch}/helm" "$dest/helm"
chmod +x "$dest/kind" "$dest/kubectl" "$dest/helm"
if ! command -v docker >/dev/null && [[ "$os" == linux ]]; then
  docker_arch=$arch; [[ "$arch" != arm64 ]] || docker_arch=aarch64
  docker_arch=${docker_arch/amd64/x86_64}
  curl -fsSL "https://download.docker.com/linux/static/stable/${docker_arch}/docker-29.0.4.tgz" -o /tmp/scalepilot-docker.tgz
  tar -xzf /tmp/scalepilot-docker.tgz -C /tmp
  cp /tmp/docker/docker "$dest/docker"
fi
if command -v docker >/dev/null && ! docker buildx version >/dev/null 2>&1; then
  mkdir -p "$HOME/.docker/cli-plugins"
  curl -fsSL "https://github.com/docker/buildx/releases/download/v0.38.1/buildx-v0.38.1.${os}-${arch}" -o "$HOME/.docker/cli-plugins/docker-buildx"
  chmod +x "$HOME/.docker/cli-plugins/docker-buildx"
fi
go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.23.0
kind version
helm version --short
kubectl version --client
