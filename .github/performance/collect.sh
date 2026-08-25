#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 3 ]]; then
	echo "usage: collect.sh BASELINE_DIR CANDIDATE_DIR OUTPUT_JSONL" >&2
	exit 2
fi

baseline_dir="$1"
candidate_dir="$2"
output_jsonl="$3"
samples="${PERFORMANCE_SAMPLES:-10}"
toolchain="${PERFORMANCE_TOOLCHAIN:-devbox}"
fixture_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

baseline_fixture="$baseline_dir/performance_fixture_test.go"
baseline_api="$baseline_dir/performance_api_test.go"
candidate_fixture="$candidate_dir/performance_fixture_test.go"
candidate_api="$candidate_dir/performance_api_test.go"
candidate_confirmation="$candidate_dir/performance_confirmation_test.go"

cleanup() {
	rm -f "$baseline_fixture" "$baseline_api" "$candidate_fixture" "$candidate_api" "$candidate_confirmation"
}
trap cleanup EXIT

cp "$fixture_dir/fixture_common_test.go" "$baseline_fixture"
cp "$fixture_dir/baseline_api_test.go" "$baseline_api"
cp "$fixture_dir/fixture_common_test.go" "$candidate_fixture"
cp "$fixture_dir/candidate_api_test.go" "$candidate_api"
cp "$fixture_dir/confirmation_test.go" "$candidate_confirmation"
: > "$output_jsonl"

run_sample() {
	local directory="$1"
	local label="$2"
	local revision="$3"
	local sample="$4"
	local log
	log="$(mktemp)"
	if ! (
		cd "$directory"
		if [[ "$toolchain" == "devbox" ]]; then
			OOZE_PERFORMANCE_LABEL="$label" \
			OOZE_PERFORMANCE_REVISION="$revision" \
			OOZE_PERFORMANCE_SAMPLE="$sample" \
			devbox run -- go test -count=1 -v -run '^TestPerformanceEvidence$' .
		elif [[ "$toolchain" == "raw-go" ]]; then
			OOZE_PERFORMANCE_LABEL="$label" \
			OOZE_PERFORMANCE_REVISION="$revision" \
			OOZE_PERFORMANCE_SAMPLE="$sample" \
			go test -count=1 -v -run '^TestPerformanceEvidence$' .
		else
			echo "unsupported performance toolchain: $toolchain" >&2
			exit 2
		fi
	) > "$log" 2>&1; then
		cat "$log" >&2
		exit 1
	fi
	local records
	records="$(grep -c '^OOZE_PERF ' "$log")"
	if [[ "$records" -ne 1 ]]; then
		echo "sample $sample for $label emitted $records records, want one" >&2
		exit 1
	fi
	sed -n 's/^OOZE_PERF //p' "$log" >> "$output_jsonl"
	rm -f "$log"
}

baseline_revision="$(git -C "$baseline_dir" rev-parse HEAD)"
candidate_revision="$(git -C "$candidate_dir" rev-parse HEAD)"

for ((sample = 1; sample <= samples; sample++)); do
	if ((sample % 2 == 1)); then
		run_sample "$baseline_dir" baseline "$baseline_revision" "$sample"
		run_sample "$candidate_dir" candidate "$candidate_revision" "$sample"
	else
		run_sample "$candidate_dir" candidate "$candidate_revision" "$sample"
		run_sample "$baseline_dir" baseline "$baseline_revision" "$sample"
	fi
done

confirmation_log="$(mktemp)"
if ! (
	cd "$candidate_dir"
	if [[ "$toolchain" == "devbox" ]]; then
		OOZE_PERFORMANCE_REVISION="$candidate_revision" \
			devbox run -- go test -count=1 -v -run '^TestPerformanceConfirmationEvidence$' .
	elif [[ "$toolchain" == "raw-go" ]]; then
		OOZE_PERFORMANCE_REVISION="$candidate_revision" \
			go test -count=1 -v -run '^TestPerformanceConfirmationEvidence$' .
	else
		echo "unsupported performance toolchain: $toolchain" >&2
		exit 2
	fi
) > "$confirmation_log" 2>&1; then
	cat "$confirmation_log" >&2
	exit 1
fi
confirmation_records="$(grep -c '^OOZE_CONFIRMATION ' "$confirmation_log")"
if [[ "$confirmation_records" -ne 1 ]]; then
	echo "confirmation evidence emitted $confirmation_records records, want one" >&2
	exit 1
fi
sed -n 's/^OOZE_CONFIRMATION //p' "$confirmation_log" >> "$output_jsonl"
rm -f "$confirmation_log"

baseline_records="$(grep -c '"label":"baseline"' "$output_jsonl")"
candidate_records="$(grep -c '"label":"candidate"' "$output_jsonl")"
if [[ "$baseline_records" -ne "$samples" || "$candidate_records" -ne "$samples" ]]; then
	echo "raw sample counts baseline=$baseline_records candidate=$candidate_records, want $samples each" >&2
	exit 1
fi
