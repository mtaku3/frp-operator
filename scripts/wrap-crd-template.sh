#!/usr/bin/env bash
# Inject additionalAnnotations/additionalLabels Helm template blocks into a
# controller-gen CRD's metadata. Read input, write output. Idempotent: if
# the input already has the blocks, output equals input.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <input.yaml> <output.yaml>" >&2
  exit 2
fi

in=$1
out=$2

awk '
  BEGIN { in_meta = 0; injected_anno = 0; injected_labels = 0; skip_next = 0 }
  skip_next { skip_next = 0; next }
  /^metadata:/ { in_meta = 1; print; next }
  in_meta && /^  annotations:/ {
    print
    # Check if next line is already the injected template
    getline
    if ($0 ~ /^    {{- with .Values.additionalAnnotations }}/) {
      # Already injected, just print through the end
      print
      getline
      print
      getline
      print
      injected_anno = 1
      next
    } else {
      # Not injected yet, inject and print current line
      print "    {{- with .Values.additionalAnnotations }}"
      print "      {{- toYaml . | nindent 4 }}"
      print "    {{- end }}"
      injected_anno = 1
      print
      next
    }
  }
  in_meta && /^  name:/ && !injected_labels {
    print "  labels:"
    print "    {{- with .Values.additionalLabels }}"
    print "      {{- toYaml . | nindent 4 }}"
    print "    {{- end }}"
    injected_labels = 1
    # Now print the current line (name:)
    print
    next
  }
  in_meta && /^  labels:/ {
    # Already has labels section, skip the new injection
    injected_labels = 1
    print
    next
  }
  /^[a-zA-Z]/ && !/^metadata:/ { in_meta = 0 }
  { print }
' "$in" > "$out"
