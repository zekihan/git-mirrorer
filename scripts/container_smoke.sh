#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
image=${MIRROR_SMOKE_IMAGE:-git-mirrorer:smoke}
docker build --tag "$image" .
# Keep synthetic repositories under the image's private, service-owned /data.
# A shared writable bind-mount ancestor is deliberately no longer accepted.
docker run --rm --network none --cpus 2 --memory 512m --entrypoint sh "$image" -ec '
    test "$(id -u)" != 0
    git lfs version
    mkdir -m 700 /data/fixture
    cd /data/fixture
    git init --initial-branch=main source
    git -C source config user.name "Container Test"
    git -C source config user.email container@example.invalid
    git -C source -c commit.gpgsign=false commit --allow-empty -m "Fixture"
    git init --bare --initial-branch=main destination
    cat > config.yaml <<YAML
stateDir: /data/fixture/state
repositories:
  - name: source
    source: /data/fixture/source
    destinations:
      - name: destination
        url: /data/fixture/destination
YAML
    git-mirrorer --config config.yaml --format json > report.json
    test "$(git -C source for-each-ref --format="%(refname) %(objectname)")" = \
         "$(git -C destination for-each-ref --format="%(refname) %(objectname)")"
    test -d state/repositories
    sed "s@source: /data/fixture/source@source: /data/fixture/missing@" config.yaml > failed.yaml
    set +e
    git-mirrorer --config failed.yaml > failed.txt
    status=$?
    set -e
    test "$status" = 1
'
test "$(docker image inspect --format '{{json .Config.ExposedPorts}}' "$image")" = null
echo "Container smoke test passed"
