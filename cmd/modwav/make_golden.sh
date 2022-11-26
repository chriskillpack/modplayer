#!/usr/bin/env bash
set -o pipefail
set -x

MODS=("space_debris" "dope" "believe")
OUTDIR="golden"

# Check if the working tree is dirty
if ! $( git diff --quiet );
then
  echo "working tree is dirty, please stash changes"
  exit 1
fi

if [ ! -d $OUTDIR ];
then
  mkdir $OUTDIR
fi

# For each MOD generate the golden wav
for mod in "${MODS[@]}"
do
  MOD_IN="../../mods/$mod.mod"
  WAV_OUT="$OUTDIR/${mod}_golden.wav"
  go run . -wav $WAV_OUT $MOD_IN > /dev/null
  if [ ! $? -eq 0 ];
  then
    echo "Failed to generate $WAV_OUT"
  fi
done