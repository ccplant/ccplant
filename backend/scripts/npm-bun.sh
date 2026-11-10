#!/bin/sh
set -eu

# The agent image uses Bun in place of npm. Translate the npm options used by
# isolated local installs rather than letting Bun treat the prefix as a package.
# Rotate only the original arguments so quoting is preserved without eval.
remaining=$#
prefix=
literal=false
while [ "$remaining" -gt 0 ]; do
    argument=$1
    shift
    remaining=$((remaining - 1))

    if [ "$literal" = true ]; then
        set -- "$@" "$argument"
        continue
    fi

    case "$argument" in
        --prefix)
            if [ "$remaining" -eq 0 ] || [ -z "$1" ]; then
                printf '%s\n' 'npm: --prefix requires a directory' >&2
                exit 2
            fi
            prefix=$1
            shift
            remaining=$((remaining - 1))
            ;;
        --prefix=*)
            prefix=${argument#--prefix=}
            if [ -z "$prefix" ]; then
                printf '%s\n' 'npm: --prefix requires a directory' >&2
                exit 2
            fi
            ;;
        --legacy-peer-deps)
            set -- "$@" --omit=peer
            ;;
        --)
            literal=true
            set -- "$@" "$argument"
            ;;
        *)
            set -- "$@" "$argument"
            ;;
    esac
done

# Change directory ourselves: Bun 1.3.5 does not accept --cwd before every
# subcommand. npm also creates a missing local installation prefix.
if [ -n "$prefix" ]; then
    mkdir -p -- "$prefix"
    cd -- "$prefix"
fi

exec env BUN_BE_BUN=1 bun "$@"
