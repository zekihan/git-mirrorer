#!/usr/bin/env bash

set -e

get_latest_release() {
	repo=$1
	res=$(curl -L -s -w "%{http_code}" "https://api.github.com/repos/${repo}/releases/latest" -o v.txt)
	if [ "$res" -ne 200 ]; then
		echo ""
	else
		version=$(cat v.txt | jq .tag_name --raw-output)
	fi
}

if ! command -v jq &>/dev/null; then
	sudo apt-get update && sudo apt-get install jq -y
fi

ARCH=$(uname -m)
if ! docker buildx &>/dev/null; then
	latest=$(get_latest_release "docker/buildx")
	version="v0.16.2"
	if [ -z "$latest" ]; then
		echo "Failed to get latest buildx version"
	else
		version=$latest
	fi
	case $ARCH in
	"x86_64")
		url="https://github.com/docker/buildx/releases/latest/download/buildx-${latest}.linux-amd64"
		;;
	"aarch64")
		url="https://github.com/docker/buildx/releases/latest/download/buildx-${latest}.linux-arm64"
		;;
	*)
		echo "Unknown architecture: ${ARCH}"
		exit 1
		;;
	esac
	bin_dir=/usr/local/lib/docker/cli-plugins
	sudo mkdir -p "${bin_dir}"
	echo "Downloading buildx version ${latest} from ${url}"
	sudo curl -sSL "${url}" -o "${bin_dir}/docker-buildx"
	sudo chmod +x "${bin_dir}/docker-buildx"
fi

if ! command -v kubectl &>/dev/null; then
	res=$(curl -L -s -w "%{http_code}" "https://dl.k8s.io/release/stable.txt" -o v.txt)
	version="v1.31.0"
	if [ "$res" -ne 200 ]; then
		echo "Failed to get latest kubectl version"
	else
		version=$(cat v.txt)
	fi
	case $ARCH in
	"x86_64")
		url="https://dl.k8s.io/release/${version}/bin/linux/amd64/kubectl"
		;;
	"aarch64")
		url="https://dl.k8s.io/release/${version}/bin/linux/arm64/kubectl"
		;;
	*)
		echo "Unknown architecture: ${ARCH}"
		exit 1
		;;
	esac
	bin_dir=/usr/local/bin/kubectl
	echo "Downloading kubectl version ${version} from ${url}"
	sudo curl -sSL "${url}" -o "${bin_dir}"
	sudo chmod +x "${bin_dir}"
fi

if ! command -v uuidgen &>/dev/null; then
	sudo apt-get update && sudo apt-get install uuid-runtime -y
fi

mkdir -p "${HOME}/.kube"

echo "${KUBECONFIG_DATA}" >"${HOME}/.kube/config"

UUID=$(uuidgen)
echo "${UUID}"

docker buildx create \
	--name "builder-${UUID}" \
	--node "builder-amd64-${UUID}" \
	--platform linux/amd64 \
	--driver kubernetes \
	'--driver-opt="namespace=buildkit","nodeselector=kubernetes.io/arch=amd64","tolerations=key=node.kubernetes.io/role,value=runner,effect=NoSchedule"'

docker buildx create --append \
	--name "builder-${UUID}" \
	--node "builder-arm64-${UUID}" \
	--platform linux/arm64 \
	--driver kubernetes \
	'--driver-opt="namespace=buildkit","nodeselector=kubernetes.io/arch=arm64","tolerations=key=node.kubernetes.io/role,value=runner,effect=NoSchedule"'

docker buildx use "builder-${UUID}"

docker buildx inspect --bootstrap

{
	echo "name=builder-${UUID}"
	echo "UUID=${UUID}"
} >>"$GITHUB_OUTPUT"
