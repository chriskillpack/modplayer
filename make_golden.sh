#!/usr/bin/env bash
# Generates golden WAV files from mods, useful for testing. By default this
# script will exit if working tree is dirty, use -d to skip this check.
# This script should be run from the project root.

set -o pipefail

SONGS=("space_debris.mod" "dope.mod" "believe.mod" "caero.s3m")
OUTDIR="golden"
skipDirty=false

# Strip the extension (.abc) from the input parameter
strip_extension() {
  filename="$1"
  echo "${filename%.*}"
}

while getopts "d" flag
do
  case "${flag}" in
    d) skipDirty=true;;
    *) echo "usage: $0 [-d]" >&2
       exit 1 ;;
  esac
done

# Check if the working tree is dirty
# dirtyTree will be either 0 or 1. The single line command below works because
# the command prints nothing to stdout.
dirtyTree=$( git diff --quiet )$?

if [ $skipDirty == false ] && [ "$dirtyTree" -eq 1 ];
then
  echo "working tree is dirty, please stash changes"
  exit 1
fi

if [ ! -d $OUTDIR ]; then
  mkdir $OUTDIR
fi

# For each MOD generate the golden wav
for song in "${SONGS[@]}"
do
  SONG_NO_EXT=$(strip_extension $song)
  SONG_FILENAME="mods/$song"
  WAV_OUT="$OUTDIR/${SONG_NO_EXT}_golden.wav"

  echo "Generating $WAV_OUT"
  go run ./cmd/modwav -reverb none -wav "$WAV_OUT" "$SONG_FILENAME" > /dev/null

  retVal=$?
  if [ $retVal -ne 0 ]; then
    echo -e "\nFailed to generate $WAV_OUT"
    exit $retVal
  fi
done

# shellcheck disable=SC2086
exit $retVal