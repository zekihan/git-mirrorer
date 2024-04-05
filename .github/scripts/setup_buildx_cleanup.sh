#!/usr/bin/env bash

kubectl delete -n buildkit deployment "builder-amd64-$UUID"
kubectl delete -n buildkit deployment "builder-arm64-$UUID"
rm -rf "$HOME/.kube/config"
