#!/usr/bin/env bash
set -euo pipefail

#####################################################################
# Deregister / delete ECS task definition revisions from a CSV file.
#
# Reads family:revision identifiers from a CSV, performs the action,
# and updates the CSV in place to track progress. Resumable — just
# re-run the same command and already-processed entries are skipped.
#
# CSV format (first row is header):
#   Identifier,deregistered,deleted
#   QA-USE1-SPDTAT-linux-chrome-100-0:1,,
#
# Usage:
#   ./delete-task-definitions.sh [OPTIONS] <CSV_FILE>
#
# Actions (pick one):
#   --deregister      Deregister ACTIVE revisions (marks deregistered column)
#   --delete          Delete INACTIVE revisions in batches (marks deleted column)
#
# Options:
#   --region REGION   AWS region (default: us-east-1)
#   --batch-size N    Revisions per delete API call, max 10 (default: 10)
#   --retry           Re-process previously failed entries (marked "false")
#   --help            Show this help
#
# Examples:
#   ./delete-task-definitions.sh --deregister --region us-east-1 definitions.csv
#   ./delete-task-definitions.sh --delete --region us-east-1 definitions.csv
#
# Retry mode:
#   ./delete-task-definitions.sh --deregister --region us-east-1 --retry definitions.csv
#   ./delete-task-definitions.sh --delete --region us-east-1 --retry definitions.csv
#####################################################################

REGION="us-east-1"
BATCH_SIZE=10
RETRY=false
ACTION=""
CSV_FILE=""

usage() {
    head -n 36 "$0" | tail -n +3 | sed 's/^# \?//'
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --deregister) ACTION="deregister"; shift ;;
        --delete)     ACTION="delete"; shift ;;
        --region)     REGION="$2"; shift 2 ;;
        --batch-size) BATCH_SIZE="$2"; shift 2 ;;
        --retry)      RETRY=true; shift ;;
        --help|-h)    usage ;;
        -*)           echo "Unknown option: $1" >&2; exit 1 ;;
        *)            CSV_FILE="$1"; shift ;;
    esac
done

if [[ -z "$ACTION" ]]; then
    echo "ERROR: Specify --deregister or --delete" >&2; exit 1
fi
if [[ -z "$CSV_FILE" ]]; then
    echo "ERROR: Provide a CSV file" >&2; exit 1
fi
if [[ ! -f "$CSV_FILE" ]]; then
    echo "ERROR: File not found: $CSV_FILE" >&2; exit 1
fi
if [[ $BATCH_SIZE -gt 10 ]]; then
    echo "ERROR: batch-size cannot exceed 10 (AWS API limit)" >&2; exit 1
fi

if [[ "$ACTION" == "deregister" ]]; then
    COL=2
    ACTION_LABEL="DEREGISTER"
else
    COL=3
    ACTION_LABEL="DELETE"
fi

update_line() {
    local file="$1" line_num="$2" new_line="$3"
    awk -v ln="$line_num" -v nl="$new_line" \
        'NR==ln{print nl; next}{print}' "$file" > "${file}.tmp" \
        && mv "${file}.tmp" "$file"
}

TOTAL=$(tail -n +2 "$CSV_FILE" | grep -c '[^[:space:]]' || true)
SUCCEEDED=$(awk -F',' -v col="$COL" 'NR>1 && $col == "true" {c++} END {print c+0}' "$CSV_FILE")
FAILED_COUNT=$(awk -F',' -v col="$COL" 'NR>1 && $col == "false" {c++} END {print c+0}' "$CSV_FILE")
NOT_PROCESSED=$((TOTAL - SUCCEEDED - FAILED_COUNT))

if [[ "$RETRY" == true ]]; then
    REMAINING=$FAILED_COUNT
else
    REMAINING=$((TOTAL - SUCCEEDED - FAILED_COUNT))
fi

echo "================================================================"
echo " ECS Task Definition Cleanup"
echo "================================================================"
echo " CSV:        $CSV_FILE"
echo " Region:     $REGION"
echo " Action:     $ACTION_LABEL"
echo " Total:      $TOTAL"
echo " Succeeded:  $SUCCEEDED"
echo " Failed:     $FAILED_COUNT"
echo " Pending:    $NOT_PROCESSED"
echo " Remaining:  $REMAINING"
if [[ "$RETRY" == true ]]; then
echo " Retry:      true"
fi
echo "================================================================"
echo ""

if [[ $REMAINING -eq 0 ]]; then
    echo "All entries already processed. Nothing to do."
    exit 0
fi

read -r -p "$ACTION_LABEL $REMAINING task definitions? [y/N] " confirm
if [[ ! "$confirm" =~ ^[yY]$ ]]; then
    echo "Aborted."
    exit 0
fi

echo ""
SUCCESS=0
FAILED=0
COUNTER=0
START_TIME=$(date +%s)

if [[ "$ACTION" == "deregister" ]]; then
    LINE_NUM=1
    while IFS=',' read -r identifier deregistered deleted rest || [[ -n "$identifier" ]]; do
        identifier="${identifier//$'\r'/}"
        deregistered="${deregistered//$'\r'/}"
        deleted="${deleted//$'\r'/}"
        LINE_NUM=$((LINE_NUM + 1))
        [[ -z "$identifier" ]] && continue
        if [[ "$RETRY" == true ]]; then
            [[ "$deregistered" != "false" ]] && continue
        else
            [[ -n "$deregistered" ]] && continue
        fi

        COUNTER=$((COUNTER + 1))

        ELAPSED=$(( $(date +%s) - START_TIME ))
        if [[ $ELAPSED -gt 0 && $COUNTER -gt 1 ]]; then
            ETA_S=$(awk -v r="$REMAINING" -v c="$((COUNTER-1))" -v e="$ELAPSED" 'BEGIN {s=(r-c)*(e/c); printf "%d", s}')
            if [[ $ETA_S -ge 3600 ]]; then
                ETA=$(printf " ETA %dh%02dm" $((ETA_S/3600)) $(( (ETA_S%3600)/60 )))
            elif [[ $ETA_S -ge 60 ]]; then
                ETA=$(printf " ETA %dm%02ds" $((ETA_S/60)) $((ETA_S%60)))
            else
                ETA=" ETA ${ETA_S}s"
            fi
        else
            ETA=""
        fi

        echo -n "[$COUNTER/$REMAINING] Deregistering $identifier... "
        if output=$(aws ecs deregister-task-definition \
            --region "$REGION" \
            --task-definition "$identifier" \
            --output text \
            --query "taskDefinition.taskDefinitionArn" 2>&1); then
            echo "ok${ETA}"
            SUCCESS=$((SUCCESS + 1))
            update_line "$CSV_FILE" "$LINE_NUM" "${identifier},true,${deleted}"
        else
            echo "FAILED"
            while IFS= read -r line; do
                [[ -n "$line" ]] && echo "  ERROR: $line" >&2
            done <<< "$output"
            FAILED=$((FAILED + 1))
            update_line "$CSV_FILE" "$LINE_NUM" "${identifier},false,${deleted}"
        fi

        sleep 1
    done < <(tail -n +2 "$CSV_FILE")

else
    BATCH_IDS=()
    BATCH_LINES=()
    BATCH_ROWS=()

    flush_batch() {
        [[ ${#BATCH_IDS[@]} -eq 0 ]] && return
        local batch_size=${#BATCH_IDS[@]}

        if output=$(aws ecs delete-task-definitions \
            --region "$REGION" \
            --task-definitions "${BATCH_IDS[@]}" \
            --query "failures[].[arn,reason,detail]" \
            --output text 2>&1); then
            failed_arns=""
            if [[ -n "$output" && "$output" != "None" ]]; then
                while IFS=$'\t' read -r fail_arn fail_reason fail_detail; do
                    [[ -z "$fail_arn" || "$fail_arn" == "None" ]] && continue
                    failed_arns="${failed_arns} ${fail_arn} "
                    if [[ -n "$fail_reason" && "$fail_reason" != "None" ]]; then
                        if [[ -n "$fail_detail" && "$fail_detail" != "None" ]]; then
                            echo "  ERROR: $fail_arn - $fail_reason ($fail_detail)" >&2
                        else
                            echo "  ERROR: $fail_arn - $fail_reason" >&2
                        fi
                    else
                        echo "  ERROR: $fail_arn" >&2
                    fi
                done <<< "$output"
            fi
            for idx in "${!BATCH_LINES[@]}"; do
                IFS=',' read -r id dereg _del <<< "${BATCH_ROWS[$idx]}"
                if [[ "$failed_arns" == *"$id"* ]]; then
                    update_line "$CSV_FILE" "${BATCH_LINES[$idx]}" "${id},${dereg},false"
                    FAILED=$((FAILED + 1))
                else
                    update_line "$CSV_FILE" "${BATCH_LINES[$idx]}" "${id},${dereg},true"
                    SUCCESS=$((SUCCESS + 1))
                fi
            done
        else
            echo "  ERROR: $output" >&2
            for idx in "${!BATCH_LINES[@]}"; do
                IFS=',' read -r id dereg _del <<< "${BATCH_ROWS[$idx]}"
                update_line "$CSV_FILE" "${BATCH_LINES[$idx]}" "${id},${dereg},false"
            done
            FAILED=$((FAILED + batch_size))
        fi

        COUNTER=$((COUNTER + batch_size))
        echo "[${COUNTER}/${REMAINING}] ok:${SUCCESS} fail:${FAILED}"

        BATCH_IDS=()
        BATCH_LINES=()
        BATCH_ROWS=()
    }

    LINE_NUM=1
    while IFS= read -r row || [[ -n "$row" ]]; do
        row="${row//$'\r'/}"
        LINE_NUM=$((LINE_NUM + 1))
        IFS=',' read -r identifier deregistered deleted rest <<< "$row"
        [[ -z "$identifier" ]] && continue
        if [[ "$RETRY" == true ]]; then
            [[ "$deleted" != "false" ]] && continue
        else
            [[ -n "$deleted" ]] && continue
        fi
        if [[ "$deregistered" != "true" ]]; then
            echo "Skipping $identifier (not deregistered)"
            continue
        fi

        BATCH_IDS+=("$identifier")
        BATCH_LINES+=("$LINE_NUM")
        BATCH_ROWS+=("$row")

        if [[ ${#BATCH_IDS[@]} -ge $BATCH_SIZE ]]; then
            flush_batch
            sleep 1
        fi
    done < <(tail -n +2 "$CSV_FILE")
    flush_batch
fi

TOTAL_ELAPSED=$(( $(date +%s) - START_TIME ))
if [[ $TOTAL_ELAPSED -ge 3600 ]]; then
    ELAPSED_FMT=$(printf "%dh%02dm%02ds" $((TOTAL_ELAPSED/3600)) $(( (TOTAL_ELAPSED%3600)/60 )) $((TOTAL_ELAPSED%60)))
elif [[ $TOTAL_ELAPSED -ge 60 ]]; then
    ELAPSED_FMT=$(printf "%dm%02ds" $((TOTAL_ELAPSED/60)) $((TOTAL_ELAPSED%60)))
else
    ELAPSED_FMT="${TOTAL_ELAPSED}s"
fi

echo ""
echo "================================================================"
echo " Done!"
echo " Action:     $ACTION_LABEL"
echo " Succeeded:  $SUCCESS"
echo " Failed:     $FAILED"
echo " Processed:  $COUNTER / $REMAINING"
echo " Elapsed:    $ELAPSED_FMT"
echo " CSV:        $CSV_FILE (updated in place)"
echo "================================================================"
