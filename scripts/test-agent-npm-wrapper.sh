#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

if [[ ${1:-} == --integration ]]; then
  # Runs in the actual agent image, using only local packages (no registry).
  fixture="$temp/fixture package"
  mkdir -p "$fixture"
  printf '%s\n' '{"name":"npm-wrapper-fixture","version":"1.0.0"}' > "$fixture/package.json"

  for variant in after before equals missing-directory; do
    caller="$temp/$variant/caller"
    target="$temp/$variant/install target"
    mkdir -p "$caller"
    if [[ $variant != missing-directory ]]; then
      mkdir -p "$target"
      printf '%s\n' '{"name":"npm-wrapper-target","private":true}' > "$target/package.json"
    fi
    printf '%s\n' '{"name":"npm-wrapper-caller","private":true}' > "$caller/package.json"
    cp "$caller/package.json" "$temp/caller.before"
    (
      cd "$caller"
      case "$variant" in
        before) npm --prefix "$target" install "$fixture" --legacy-peer-deps ;;
        equals) npm install "$fixture" "--prefix=$target" --legacy-peer-deps ;;
        *) npm install "$fixture" --prefix "$target" --legacy-peer-deps ;;
      esac
    )
    cmp -s "$caller/package.json" "$temp/caller.before" || fail "$variant: caller manifest changed"
    [[ ! -e $caller/bun.lock && ! -e $caller/bun.lockb && ! -e $caller/node_modules ]] || fail "$variant: caller directory polluted"
    [[ -f $target/node_modules/npm-wrapper-fixture/package.json ]] || fail "$variant: target installation missing"
    echo "PASS: $variant prefix installation is isolated"
  done
  exit 0
fi

[[ $# == 0 ]] || fail 'usage: test-agent-npm-wrapper.sh [--integration]'
wrapper="$repo_root/backend/scripts/npm-bun.sh"
mkdir -p "$temp/bin"
export NPM_WRAPPER_TEST_ARGS="$temp/arguments"
export NPM_WRAPPER_TEST_ENV="$temp/environment"
export NPM_WRAPPER_TEST_CWD="$temp/directory"
cat > "$temp/bin/bun" <<'STUB'
#!/bin/sh
: > "$NPM_WRAPPER_TEST_ARGS"
if [ "$#" -gt 0 ]; then
  printf '%s\0' "$@" > "$NPM_WRAPPER_TEST_ARGS"
fi
printf '%s' "$BUN_BE_BUN" > "$NPM_WRAPPER_TEST_ENV"
pwd > "$NPM_WRAPPER_TEST_CWD"
exit "${NPM_WRAPPER_TEST_EXIT:-0}"
STUB
chmod +x "$temp/bin/bun"

invoke() {
  PATH="$temp/bin:$PATH" sh "$wrapper" "$@"
}
expect_arguments() {
  : > "$temp/expected"
  if [[ $# -gt 0 ]]; then
    printf '%s\0' "$@" > "$temp/expected"
  fi
  cmp -s "$NPM_WRAPPER_TEST_ARGS" "$temp/expected" || fail 'forwarded arguments differ'
  [[ $(< "$NPM_WRAPPER_TEST_ENV") == 1 ]] || fail 'Bun runtime mode not set'
}
expect_invalid_prefix() {
  rm -f "$NPM_WRAPPER_TEST_ARGS"
  if invoke "$@" 2> "$temp/error"; then
    fail 'invalid prefix accepted'
  else
    [[ $? == 2 ]] || fail 'invalid prefix has unexpected exit code'
  fi
  grep -Fq 'npm: --prefix requires a directory' "$temp/error" || fail 'missing prefix diagnostic'
  [[ ! -e $NPM_WRAPPER_TEST_ARGS ]] || fail 'Bun ran for an invalid prefix'
}

target="$temp/install target"
invoke install 'fixture package' --prefix "$target" --legacy-peer-deps
expect_arguments install 'fixture package' --omit=peer
[[ -d $target ]] || fail 'missing prefix was not created'
[[ $(< "$NPM_WRAPPER_TEST_CWD") == "$(cd "$target" && pwd)" ]] || fail 'Bun did not run in the prefix'

invoke --prefix "$target" install '@example/tool@1.0.0'
expect_arguments install '@example/tool@1.0.0'
[[ $(< "$NPM_WRAPPER_TEST_CWD") == "$(cd "$target" && pwd)" ]] || fail 'prefix before command was ignored'

# Shell syntax must remain a literal package argument, not be evaluated.
# shellcheck disable=SC2016
invoke install "--prefix=$target" 'a "quoted" package' '$(not-executed);*'
# shellcheck disable=SC2016
expect_arguments install 'a "quoted" package' '$(not-executed);*'
[[ $(< "$NPM_WRAPPER_TEST_CWD") == "$(cd "$target" && pwd)" ]] || fail 'equals prefix was ignored'

invoke run task -- --prefix "$temp/not-created" --legacy-peer-deps
expect_arguments run task -- --prefix "$temp/not-created" --legacy-peer-deps
[[ ! -e $temp/not-created ]] || fail 'literal argument was treated as a prefix'
[[ $(< "$NPM_WRAPPER_TEST_CWD") == "$PWD" ]] || fail 'literal argument changed directory'

invoke run build -- 'argument with spaces' $'argument\nwith newline'
expect_arguments run build -- 'argument with spaces' $'argument\nwith newline'

invoke --prefix "$target" run task -- --prefix literal --legacy-peer-deps
expect_arguments run task -- --prefix literal --legacy-peer-deps
[[ $(< "$NPM_WRAPPER_TEST_CWD") == "$(cd "$target" && pwd)" ]] || fail 'prefix before literal arguments was ignored'

invoke install --prefix "$temp/unused" "--prefix=$target"
expect_arguments install
[[ ! -e $temp/unused ]] || fail 'earlier prefix was not overridden'
[[ $(< "$NPM_WRAPPER_TEST_CWD") == "$(cd "$target" && pwd)" ]] || fail 'last prefix did not win'

(
  cd "$temp"
  invoke --prefix=-relative-target install
  expect_arguments install
  [[ $(< "$NPM_WRAPPER_TEST_CWD") == "$temp/-relative-target" ]] || fail 'relative prefix was not honored'
)

invoke
expect_arguments

expect_invalid_prefix install --prefix
expect_invalid_prefix install --prefix ''
expect_invalid_prefix install --prefix=

export NPM_WRAPPER_TEST_EXIT=37
if invoke --version; then
  fail 'Bun failure was hidden'
else
  [[ $? == 37 ]] || fail 'Bun exit code was not preserved'
fi
unset NPM_WRAPPER_TEST_EXIT

echo 'npm wrapper argument tests passed'
