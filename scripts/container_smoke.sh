#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
image=${MIRROR_SMOKE_IMAGE:-git-mirrorer:smoke}
docker build --tag "$image" .
smoke_dir=$(mktemp -d)
chmod 777 "$smoke_dir"
cleanup() {
    cleanup_status=$?
    trap - EXIT HUP INT TERM
    if ! docker run --rm --network none --entrypoint sh --mount "type=bind,src=$smoke_dir,dst=/fixture" "$image" \
        -ec 'rm -rf /fixture/source /fixture/destination /fixture/state; rm -f /fixture/config.yaml'; then
        cleanup_status=1
    fi
    if ! rm -rf "$smoke_dir"; then
        cleanup_status=1
    fi
    if [ "$cleanup_status" -eq 0 ]; then
        echo "Container smoke test passed"
    fi
    exit "$cleanup_status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

docker run --rm --entrypoint sh --mount "type=bind,src=$smoke_dir,dst=/fixture" "$image" -ec '
    test "$(id -u)" != 0
    git lfs version
    git init --initial-branch=main /fixture/source
    git -C /fixture/source config user.name "Container Test"
    git -C /fixture/source config user.email container@example.invalid
    git -C /fixture/source -c commit.gpgsign=false commit --allow-empty -m "Fixture"
    git init --bare --initial-branch=main /fixture/destination
    cat > /fixture/config.yaml <<EOF
stateDir: /fixture/state
repositories:
  - name: source
    source: /fixture/source
    destinations:
      - name: destination
        url: /fixture/destination
EOF
'

docker run --rm --network none --mount "type=bind,src=$smoke_dir,dst=/fixture" "$image" \
    --config /fixture/config.yaml --format json > "$smoke_dir/report.json"

docker run --rm --network none --entrypoint sh --mount "type=bind,src=$smoke_dir,dst=/fixture" "$image" -ec '
    test "$(git -C /fixture/source for-each-ref --format="%(refname) %(objectname)")" = \
         "$(git -C /fixture/destination for-each-ref --format="%(refname) %(objectname)")"
    test -d /fixture/state/repositories
'
test "$(docker image inspect --format '{{json .Config.ExposedPorts}}' "$image")" = null
sed 's@source: /fixture/source@source: /fixture/missing@' "$smoke_dir/config.yaml" > "$smoke_dir/failed.yaml"
set +e
docker run --rm --network none --mount "type=bind,src=$smoke_dir,dst=/fixture" "$image" \
    --config /fixture/failed.yaml > "$smoke_dir/failed.txt"
status=$?
set -e
test "$status" = 1
