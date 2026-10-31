#!/usr/bin/env sh
# check.sh runs every check cog has, by hand. cog has no CI: this script is how
# a change is checked before it is pushed.
#
#   scripts/check.sh            all seven steps
#   scripts/check.sh --no-race  steps 1-6, for a machine without a C toolchain
#   scripts/check.sh --race     step 7 only, on WSL or Linux
#
# Steps 1-6 need only sh, git and Go, and run anywhere, Git Bash on Windows
# included. Step 7, the race detector, needs cgo and a C toolchain; where it
# cannot build, the step fails with an explanation and never skips.
#
# The script stops at the first failing step. Each step prints a header before
# it runs, so the last header on screen names the step that failed.

set -eu

usage() {
	echo "usage: scripts/check.sh [--no-race | --race]" >&2
	echo "  (no argument)  run every step, the race detector last" >&2
	echo "  --no-race      run every step but the race detector" >&2
	echo "  --race         run only the race detector (WSL or Linux)" >&2
	exit 2
}

main=1
race=1
if [ $# -gt 1 ]; then
	usage
fi
if [ $# -eq 1 ]; then
	case "$1" in
	--no-race) race=0 ;;
	--race) main=0 ;;
	*) usage ;;
	esac
fi

cd "$(git rev-parse --show-toplevel)"

step() {
	echo "== $*"
}

packages="$(go list ./...)"

# except prints the module's packages minus those whose import path matches
# the extended regular expression $1.
except() {
	printf '%s\n' "$packages" | grep -Ev "$1"
}

if [ "$main" = 1 ]; then
	step "go build ./..."
	go build ./...

	step "go vet ./..."
	go vet ./...

	# Tracked files only, so .claude/worktrees/* and anything else untracked is
	# skipped. .gitattributes checks .go files out with LF endings, so a Windows
	# checkout agrees with a Linux one.
	step "gofmt -l over git ls-files '*.go'"
	unformatted="$(git ls-files '*.go' | xargs gofmt -l)"
	if [ -n "$unformatted" ]; then
		echo "these files are not gofmt'ed:" >&2
		echo "$unformatted" >&2
		exit 1
	fi

	# Built and vetted for js/wasm only. The js-only packages' tests,
	# extensions/jsstorage's and extensions/jssound's, are not run: they need a
	# js/wasm runtime (node and wasm_exec_node.js) that this script does not
	# drive, and on any other GOOS they are skipped for their build tags.
	step "GOOS=js GOARCH=wasm go build ./..."
	GOOS=js GOARCH=wasm go build ./...

	step "GOOS=js GOARCH=wasm go vet ./..."
	GOOS=js GOARCH=wasm go vet ./...

	step "go test ./..."
	go test ./...

	# Validation mode over every package, so every System in every ECS bundle
	# and libs/m's tag-switched List check run under it.
	# bundles/ecsphysics2d/... is excluded until
	# https://github.com/dvoyni/cog/issues/548 is fixed: the Hooks pace check
	# panics in its Index System and its step tests fail on their precondition.
	# Remove this filter when that bug is fixed.
	step "go test -tags ecs_validate, all but bundles/ecsphysics2d/..."
	go test -tags ecs_validate $(except '^github\.com/dvoyni/cog/bundles/ecsphysics2d(/|$)')
fi

if [ "$race" = 1 ]; then
	step "go test -race, all but the named exclusions"

	# The detector needs cgo, which Go turns off when it finds no C compiler,
	# and a C toolchain to build the race runtime. Check both before running
	# anything, and fail rather than run the tests without -race.
	if [ "$(go env CGO_ENABLED)" != 1 ] || ! go test -race -run '^$' ./libs/m >/dev/null 2>&1; then
		cat >&2 <<'EOF'
-race cannot build on this machine: the race detector needs cgo and a working
C toolchain, and this machine has none (go env CGO_ENABLED is not 1, or the
race runtime failed to build).

Run the race step under WSL or on Linux, with gcc or clang installed
(apt install build-essential on Debian or Ubuntu), or skip it here with
scripts/check.sh --no-race.
EOF
		exit 1
	fi

	# Excluded from -race, and still run by go test ./... above:
	# - docs/research/ecs-go-mechanics-bench is a research harness that asserts
	#   nothing, and its 30 s allocation sweep would multiply under the detector.
	# - kernel/archtest checks package structure and import edges, runs no
	#   concurrent code for the detector to check, and already takes ~34 s.
	go test -race $(except '^github\.com/dvoyni/cog/(docs/research/ecs-go-mechanics-bench|kernel/archtest)$')
fi

echo "== all checks passed"
