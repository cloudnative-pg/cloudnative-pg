#!/bin/bash
export LANG=en

# Configuration
REPORT_FILE="LABEL_AUDIT_REPORT.md"
SEARCH_DIR="./e2e"
SOURCE_FILE="labels.go"

# 1. Setup & Pre-flight checks
if [ ! -d "$SEARCH_DIR" ]; then
    echo "Error: Directory $SEARCH_DIR not found."
    exit 1
fi

# Extract and sort unique labels once
LABELS=$(awk -F'=' '/Label/ && /=/ {gsub(/[[:space:]]+/, "", $1); print $1}' "$SOURCE_FILE" | sort -u)

# Initialize the Report
{
    echo "# E2E Labeling Audit Report"
    echo "Generated on: $(date)"
    echo ""
    echo "## Table of Contents"
    echo "* [1. Label Usage Summary](#1-label-usage-summary)"
    echo "* [2. Detailed Breakdown by Label](#2-detailed-breakdown-by-label)"
    echo "* [3. Detailed Breakdown by File](#3-detailed-breakdown-by-file)"
    echo ""
    echo "## 1. Label Usage Summary"
    echo "| Label Name | File Count | Total Matches |"
    echo "| :--- | :---: | :---: |"
} > "$REPORT_FILE"

# Temp files for detailed sections to manage report order
TEMP_BY_LABEL=$(mktemp)
TEMP_BY_FILE=$(mktemp)

echo "## 2. Detailed Breakdown by Label" > "$TEMP_BY_LABEL"
echo "## 3. Detailed Breakdown by File" > "$TEMP_BY_FILE"

# --- SECTION 1 & 2: PROCESS BY LABEL ---
for label in $LABELS; do
    grep_data=$(grep -Rwc "$label" "$SEARCH_DIR" | grep -v ":0$")

    if [ -n "$grep_data" ]; then
        stats=$(echo "$grep_data" | awk -F: '{files++; matches += $2} END {print files","matches}')
        f_count=$(echo "$stats" | cut -d',' -f1)
        m_count=$(echo "$stats" | cut -d',' -f2)
    else
        f_count=0
        m_count=0
    fi

    # 1. Summary Table Row
    printf "| %-30s | %-10d | %-13d |\n" "$label" "$f_count" "$m_count" >> "$REPORT_FILE"

    # 2. Detailed Label Breakdown
    {
        echo "### Label: \`$label\`"
        if [ "$f_count" -gt 0 ]; then
            echo "$grep_data" | awk -F: '{printf "    - \`%s\`: %d matches\n", $1, $2}'
            echo ""
            echo "**Summary:** Found in $f_count files with $m_count total occurrences."
        else
            echo "    - *No occurrences found in $SEARCH_DIR*"
        fi
        echo ""
    } >> "$TEMP_BY_LABEL"
done

# --- SECTION 3: PROCESS BY FILE ---
find "$SEARCH_DIR" -name "*.go" | sort | while read -r test_file; do
    file_content=$(cat "$test_file")
    file_label_details=""
    file_total_labels=0
    file_total_matches=0
    
    for label in $LABELS; do
        count=$(echo "$file_content" | grep -ow "$label" | wc -l)
        if [ "$count" -gt 0 ]; then
            file_label_details="${file_label_details}    - \`$label\`: $count matches\n"
            ((file_total_labels++))
            ((file_total_matches+=count))
        fi
    done

    if [ $file_total_labels -gt 0 ]; then
        {
            echo "### File: \`$test_file\`"
            echo -e "$file_label_details"
            echo "**Summary:** Contains $file_total_labels unique labels with $file_total_matches total occurrences."
            echo ""
        } >> "$TEMP_BY_FILE"
    fi
done

# Assemble final report
echo "" >> "$REPORT_FILE"
cat "$TEMP_BY_LABEL" >> "$REPORT_FILE"
echo "---" >> "$REPORT_FILE"
cat "$TEMP_BY_FILE" >> "$REPORT_FILE"

# Cleanup
rm "$TEMP_BY_LABEL" "$TEMP_BY_FILE"

echo "Success! Unified detailed report generated: $REPORT_FILE"
