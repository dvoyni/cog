#!/usr/bin/env bash
# Interleaved A/B of the whole-step benchmarks, the continuous-collision map's
# cost harness (#328, ticket #576).
#
# Whole-frame benchmarks here swing about ±10% by run order, so a before/after
# pair run one after the other invents regressions. This builds nothing: it
# takes two test binaries already built, one per side, and runs them in ABBA
# order round by round, so neither side is always first. It prints each
# benchmark's min and median ns/op per side; the min is the least contaminated,
# since noise only ever adds time.
#
# Build each side from its own tree:
#   go test -c -o A.exe ./bundles/ecsphysics2d/internal   # the baseline tree
#   go test -c -o B.exe ./bundles/ecsphysics2d/internal   # the candidate tree
# then:
#   bundles/ecsphysics2d/internal/ab-step.sh A.exe B.exe [rounds] [bench regex]
#
# Two copies of the same binary give the noise floor any difference has to
# clear.
set -euo pipefail

a=$1
b=$2
rounds=${3:-10}
bench=${4:-'^Benchmark(TheStep|ThePolygonStep)$'}
out=$(mktemp -d)

run() { "$1" -test.run '^$' -test.bench "$bench" -test.benchtime 2s -test.count 1 >>"$2"; }

for ((i = 0; i < rounds; i++)); do
	if ((i % 2 == 0)); then
		run "$a" "$out/a.txt"
		run "$b" "$out/b.txt"
	else
		run "$b" "$out/b.txt"
		run "$a" "$out/a.txt"
	fi
done

summarise() {
	grep '^Benchmark' "$1" | awk '{print $1, $3}' | sort -k1,1 -k2,2n | awk '
		{ name = $1; v[name] = v[name] " " $2; n[name]++ }
		END {
			for (name in v) {
				split(substr(v[name], 2), s, " ")
				printf "%-40s min %10.0f  median %10.0f  (%d runs)\n", name, s[1], s[int((n[name] + 1) / 2)], n[name]
			}
		}' | sort
}

echo "A: $a"
summarise "$out/a.txt"
echo "B: $b"
summarise "$out/b.txt"
echo "raw runs: $out"
