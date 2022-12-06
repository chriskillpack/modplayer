#!/usr/bin/env bash

# A script to verify player output against pre-generated golden files
# It's not very robust and serves only as a quick end-to-end test
# Generate the golden files with make_golden.sh first.
set -o pipefail

MODS=("space_debris" "dope" "believe")
GOLDENDIR="golden"

if [ ! -d $GOLDENDIR ];
then
    echo "Could not find golden directory '$GOLDENDIR', stopping"
    exit 1
fi

TMPDIR=`mktemp -d` || exit 1

for mod in "${MODS[@]}"
do
  MOD_IN="../../mods/$mod.mod"
  WAV_OUT="$TMPDIR/$mod.wav"
  GOLDEN_FILE="$GOLDENDIR/${mod}_golden.wav"
  echo "Checking $mod.mod"

  # Generate the candidate WAV file
  go run . -wav $WAV_OUT $MOD_IN > /dev/null
  if [ ! $? -eq 0 ];
  then
    echo "Failed to generate $WAV_OUT"
  fi

  # Compare the candidate against the golden version
  cmp -s $WAV_OUT $GOLDEN_FILE
  if [ ! $? -eq 0 ];
  then
    echo "!!! $mod does not match golden, see ${GOLDEN_FILE} and ${WAV_OUT}"
    exit 1
  fi
done