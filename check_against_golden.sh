#!/usr/bin/env bash
# A script to verify the player output against previously generated golden files
# It is a useful end-to-end test which has caught several bugs. Generate the
# golden files with make_golden.sh first. This script should be run from the
# project root.

set -o pipefail

MODS=("space_debris" "dope" "believe")
GOLDENDIR="golden"

if [ ! -d $GOLDENDIR ];
then
    echo "Could not find golden directory '$GOLDENDIR', stopping"
    exit 1
fi

TMPDIR=$(mktemp -d)

retVal=$?
if [ $retVal -ne 0 ]; then
  echo -e "\nCould not create temporary directory"
  exit $retVal
fi

missing=false

for mod in "${MODS[@]}"
do
  MOD_IN="./mods/$mod.mod"
  WAV_OUT="$TMPDIR/$mod.wav"
  GOLDEN_FILE="$GOLDENDIR/${mod}_golden.wav"
  echo -n "Checking $mod.mod..."

  # Check that the golden file exists
  if [ ! -f "$GOLDEN_FILE" ]; then
    echo " $GOLDEN_FILE does not exist, skipping"
    missing=true
    continue
  fi

  # Generate the candidate WAV file
  go run ./cmd/modwav -reverb none -wav "$WAV_OUT" "$MOD_IN" > /dev/null

  retVal=$?
  if [ $retVal -ne 0 ]; then
    echo -e "\nFailed to generate $WAV_OUT"
    exit $retVal
  fi

  # Compare the candidate against the golden version
  cmp -s "$WAV_OUT" "$GOLDEN_FILE"

  retVal=$?
  if [ $retVal -ne 0 ]; then
    echo -e "\n!!! $mod does not match golden"
    echo "cmp -l $WAV_OUT $GOLDEN_FILE"
    exit $retVal
  else
    echo  # Move to next line, see echo -n at top of loop
  fi
done

if $missing ; then
  exit 2
fi
exit $retVal