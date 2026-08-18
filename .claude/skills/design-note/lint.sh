#!/bin/sh
# Word counts are approximate. STE rules 8.5 to 8.7 count parenthetical text,
# quoted text, and hyphenated groups as one word each.

set -u

if [ $# -lt 1 ]; then
  echo "usage: lint.sh <file.md> ..." >&2
  exit 2
fi

fail=0
for file in "$@"; do
  out=$(awk '
    function flush_para(  n, parts, i, w, words) {
      if (para == "") return
      gsub(/\([^)]*\)/, "PAREN", para)
      gsub(/"[^"]*"/, "QUOTE", para)
      n = split(para, parts, /[.!?]([[:space:]]+|$)/)
      for (i = 1; i <= n; i++) {
        w = split(parts[i], words, /[[:space:]]+/)
        if (w > 25)
          printf "%s:%d: ~%d-word sentence (maximum 25; 20 for an instruction)\n", FILENAME, start, w
      }
      para = ""
    }
    BEGIN { q = sprintf("%c", 39) }
    /^```/ { flush_para(); code = !code; next }
    code { next }
    /^#/ || /^[[:space:]]*\|/ || /^---/ { flush_para(); next }
    /^[[:space:]]*$/ { flush_para(); next }
    /^[[:space:]]*([-*+]|[0-9]+[.)])[[:space:]]/ { flush_para() }
    {
      line = $0
      sub(/^[[:space:]]*([-*+]|[0-9]+[.)])[[:space:]]+/, "", line)
      sub(/^[[:space:]]*>[[:space:]]*/, "", line)
      gsub(/`[^`]*`/, "CODE", line)
      lp = " " tolower(line) " "
      if (line ~ /;/)
        printf "%s:%d: semicolon: write two sentences\n", FILENAME, FNR
      if (lp ~ /[^a-z](e\.g\.|i\.e\.|etc\.)/)
        printf "%s:%d: Latin abbreviation: write it in English\n", FILENAME, FNR
      if (line ~ ("n" q "t|" q "re |" q "ll |" q "ve |[Ii]t" q "s|[Ll]et" q "s"))
        printf "%s:%d: contraction: write the words in full\n", FILENAME, FNR
      if (match(lp, /[^a-z](utilize|leverage|ensures?|ensured|however|therefore|whilst|upon|should|prior to|in order to)[^a-z]/))
        printf "%s:%d: \"%s\": refer to word-list.md\n", FILENAME, FNR, substr(lp, RSTART + 1, RLENGTH - 2)
      if (para == "") start = FNR
      para = para " " line
    }
    END { flush_para() }
  ' "$file")
  if [ -n "$out" ]; then
    printf '%s\n' "$out"
    fail=1
  fi
done
exit $fail
