#!/usr/bin/env bash
# Generates golden WAV files from mods, useful for testing
# By default will exit if working tree is dirty, use -d to skip this check

set -o pipefail
set -x

MODS=("space_debris" "dope" "believe")
OUTDIR="golden"
skipDirty=false

while getopts d flag
do
  case "${flag}" in
    d) skipDirty=true;;
  esac
done

# Check if the working tree is dirty
# dirtyTree will be either 0 or 1. The single line command below works because
# the command prints nothing to stdout.
dirtyTree=$( git diff --quiet )$?

if [ $skipDirty == false ] && [ $dirtyTree -eq 1 ];
then
  echo "working tree is dirty, please stash changes"
  exit 1
fi

if [ ! -d $OUTDIR ]; then
  mkdir $OUTDIR
fi

# For each MOD generate the golden wav
for mod in "${MODS[@]}"
do
  MOD_IN="../../mods/$mod.mod"
  WAV_OUT="$OUTDIR/${mod}_golden.wav"
  go run . -wav $WAV_OUT $MOD_IN > /dev/null

  retVal=$?
  if [ $retVal -ne 0 ]; then
    echo -e "\nFailed to generate $WAV_OUT"
  fi
done

exit $retVal