#!/usr/bin/env bash
# A script to verify the player output against previously generated golden files
# It is a useful end-to-end test which has caught several bugs. Generate the
# golden files with make_golden.sh first. This script should be run from the
# project root.

set -o pipefail

SONGS=("space_debris.mod" "dope.mod" "believe.mod" "caero.s3m")
GOLDENDIR="golden"
GREEN_CHECK_MARK="\xe2\x9c\x85"  # UTF-8 encoding of U-2705, for some reason
                                 # using echo -e "\u2705" is not working.

# Strip the extension (.abc) from the input parameter
strip_extension() {
  filename="$1"
  echo "${filename%.*}"
}

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

for song in "${SONGS[@]}"
do
  SONG_NO_EXT=$(strip_extension $song)
  SONG_FILENAME="./mods/$song"
  WAV_OUT="$TMPDIR/$SONG_NO_EXT.wav"
  GOLDEN_FILE="$GOLDENDIR/${SONG_NO_EXT}_golden.wav"
  echo -n "Checking $song..."

  # Check that the golden file exists
  if [ ! -f "$GOLDEN_FILE" ]; then
    echo " $GOLDEN_FILE does not exist, skipping"
    missing=true
    continue
  fi

  # Generate the candidate WAV file
  go run ./cmd/modwav -reverb none -wav "$WAV_OUT" "$SONG_FILENAME" > /dev/null

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
    printf "$GREEN_CHECK_MARK\n" # Print a green check mark and move to next line, see echo -n at top of loop
  fi
done

if $missing ; then
  exit 2
fi
exit $retVal