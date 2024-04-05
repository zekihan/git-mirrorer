#!/usr/bin/env bash

set -e

get_latest_release() {
	curl --silent "https://api.github.com/repos/$1/releases/latest" |
		grep '"tag_name":' |
		sed -E 's/.*"([^"]+)".*/\1/'
}

ARCH=$(uname -m)
if ! docker buildx &>/dev/null; then
	latest=$(get_latest_release "docker/buildx")
	case $ARCH in
	"x86_64")
		URL="https://github.com/docker/buildx/releases/latest/download/buildx-$latest.linux-amd64"
		;;
	"aarch64")
		URL="https://github.com/docker/buildx/releases/latest/download/buildx-$latest.linux-arm64"
		;;
	*)
		echo "Unknown architecture: $ARCH"
		exit 1
		;;
	esac
	BIN=/usr/local/lib/docker/cli-plugins
	sudo mkdir -p "$BIN"
	sudo curl -sSL "$URL" -o "$BIN/docker-buildx"
	sudo chmod +x "$BIN/docker-buildx"
fi

if ! command -v kubectl &>/dev/null; then
	VERSION=$(curl -L -s https://dl.k8s.io/release/stable.txt)
	case $ARCH in
	"x86_64")
		URL="https://dl.k8s.io/release/$VERSION/bin/linux/amd64/kubectl"
		;;
	"aarch64")
		URL="https://dl.k8s.io/release/$VERSION/bin/linux/arm64/kubectl"
		;;
	*)
		echo "Unknown architecture: $ARCH"
		exit 1
		;;
	esac
	BIN=/usr/local/bin/kubectl
	sudo curl -sSL "$URL" -o "$BIN"
	sudo chmod +x "$BIN"
fi

if ! command -v uuidgen &>/dev/null; then
	sudo apt-get update && sudo apt-get install uuid-runtime -y
fi

mkdir -p "$HOME/.kube"

echo "$KUBECONFIG_DATA" >"$HOME/.kube/config"

UUID=$(uuidgen)
echo "$UUID"

docker buildx create \
	--name "builder-$UUID" \
	--node "builder-amd64-$UUID" \
	--platform linux/amd64 \
	--driver kubernetes \
	'--driver-opt="namespace=buildkit","nodeselector=kubernetes.io/arch=amd64","tolerations=key=node.kubernetes.io/role,value=runner,effect=NoSchedule"'

docker buildx create --append \
	--name "builder-$UUID" \
	--node "builder-arm64-$UUID" \
	--platform linux/arm64 \
	--driver kubernetes \
	'--driver-opt="namespace=buildkit","nodeselector=kubernetes.io/arch=arm64","tolerations=key=node.kubernetes.io/role,value=runner,effect=NoSchedule"'

docker buildx use "builder-$UUID"

docker buildx inspect --bootstrap

{
	echo "name=builder-$UUID"
	echo "UUID=$UUID"
} >>"$GITHUB_OUTPUT"
