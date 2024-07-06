#!/usr/bin/env bash

set -e

if ! command -v gh &>/dev/null; then
	(type -p wget >/dev/null || (sudo apt update && sudo apt-get install wget -y)) &&
		sudo mkdir -p -m 755 /etc/apt/keyrings &&
		wget -qO- https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg >/dev/null &&
		sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg &&
		echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list >/dev/null &&
		sudo apt update &&
		sudo apt install gh -y
fi

if ! command -v git-cliff &>/dev/null; then
	architecture=$(uname -m)
	gh release download --repo orhun/git-cliff --pattern "git-cliff-*-${architecture}-unknown-linux-musl.tar.gz" --clobber --dir /tmp
	mkdir -p /tmp/git-cliff
	tar -xzf /tmp/git-cliff-*-"${architecture}"-unknown-linux-musl.tar.gz -C /tmp/git-cliff
	sudo mv /tmp/git-cliff/git-cliff-*/git-cliff  /usr/local/bin/git-cliff
fi

vers=$(tr -d '[:space:]' <VERSION)
if ! git show-ref --tags --verify --quiet "refs/tags/v${vers}"; then
	release_notes=$(git-cliff --strip all --tag "v${vers}" --unreleased)
	gh release create "v${vers}" --notes "${release_notes}"
fi
