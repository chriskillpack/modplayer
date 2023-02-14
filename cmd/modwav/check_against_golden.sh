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

TMPDIR=`mktemp -d`

retVal=$?
if [ $retVal -ne 0 ]; then
  echo -e "\nCould not create temporary directory"
  exit $retVal
fi

for mod in "${MODS[@]}"
do
  MOD_IN="../../mods/$mod.mod"
  WAV_OUT="$TMPDIR/$mod.wav"
  GOLDEN_FILE="$GOLDENDIR/${mod}_golden.wav"
  echo "Checking $mod.mod"

  # Generate the candidate WAV file
  go run . -reverb none -wav $WAV_OUT $MOD_IN > /dev/null

  retVal=$?
  if [ $retVal -ne 0 ]; then
    echo -e "\nFailed to generate $WAV_OUT"
  fi

  # Compare the candidate against the golden version
  cmp -s $WAV_OUT $GOLDEN_FILE

  retVal=$?
  if [ $retVal -ne 0 ]; then
    echo -e "\n!!! $mod does not match golden, see ${GOLDEN_FILE} and ${WAV_OUT}"
    exit $retVal
  fi
done

exit $retVal