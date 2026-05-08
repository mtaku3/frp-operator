#!/usr/bin/env bash
# Copy a markdown file into the Starlight content tree, prepending the
# required `title` and `description` frontmatter. The first H1 line in
# the source is dropped (Starlight renders the title from frontmatter).
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: $0 <src.md> <dst.mdx> <title> <description>" >&2
  exit 2
fi

src=$1
dst=$2
title=$3
description=$4

mkdir -p "$(dirname "$dst")"

# Quote-escape any embedded double-quotes for YAML.
title=${title//\"/\\\"}
description=${description//\"/\\\"}

{
  printf -- '---\n'
  printf 'title: "%s"\n' "$title"
  printf 'description: "%s"\n' "$description"
  printf -- '---\n\n'
  # Strip the first leading H1 (Starlight renders title from frontmatter).
  awk 'BEGIN{stripped=0} /^# / && !stripped {stripped=1; next} {print}' "$src"
} > "$dst"
