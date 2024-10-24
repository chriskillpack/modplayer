package modplayer

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func newTestPlayerFromMod(file string) (*Player, error) {
	mod, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	song, err := NewMODSongFromBytes(mod)
	if err != nil {
		return nil, err
	}
	player, err := NewPlayer(song, 44100)
	if err != nil {
		return nil, err
	}
	return player, nil
}

func newPlayerWithTestPattern(pattern [][]string, t *testing.T) *Player {
	noteData, nChannels := convertTestPatternData(pattern)

	sampLength := 1000
	song := &Song{
		Title:        "testsong",
		Channels:     nChannels,
		GlobalVolume: 64,
		Speed:        2,
		Tempo:        125,
		Orders:       []byte{0},
		Samples: []Sample{
			{
				Name:    "testins1",
				Volume:  60,
				C4Speed: 8363,
				Length:  sampLength,
				Data:    make([]int8, sampLength),
			},
			{
				Name:    "testins2",
				Volume:  55,
				C4Speed: 8363,
				Length:  sampLength,
				Data:    make([]int8, sampLength),
			},
		},
		patterns: noteData,
	}
	player, err := NewPlayer(song, 44100)
	if err != nil {
		t.Fatalf("Could not create test player: %e", err)
		return nil
	}
	player.Start()
	return player
}

// Takes input of the form
// A-4 12 22 S34  - play A-4 with instrument 12, at volume 22 with S3M effect S with parameter 34
// ... .. 11 ...  - set volume to 11
// ^^^ .. .. ...  - note off
// <empty string> - skip
func convertTestPatternData(pattern [][]string) ([][]note, int) {
	nChannels := len(pattern[0])

	notes := make([][]note, 1)
	notes[0] = make([]note, nChannels*len(pattern))

	// Parse each row of input
	for r, row := range pattern {
		for c, col := range row {
			note := &notes[0][r*nChannels+c]

			if col == "" {
				// All the other fields are already initialized to 0
				note.Volume = noNoteVolume
				continue
			}

			// Decode note
			parts := colToParts(col)
			note.Pitch = decodeNote(parts[0])
			note.Sample = decodeInt(parts[1], 0)
			note.Volume = decodeInt(parts[2], noNoteVolume)
			note.Effect, note.Param = decodeEffect(parts[3])
		}
	}

	return notes, nChannels
}

// Advances to next row in the pattern, will have processed the first tick
// of the next row on return.
func advanceToNextRow(plr *Player) {
	old := plr.row
	for old == plr.row {
		plr.sequenceTick()
	}
}

func colToParts(s string) []string {
	result := strings.Split(s, " ")

	filtered := []string{}
	for _, r := range result {
		if r == "" {
			continue
		}
		filtered = append(filtered, r)
	}

	return filtered
}

func decodeNote(note string) playerNote {
	// note is of the form A-2, A#2, ^^. or ...
	if note == "^^." {
		return playerNote(noteKeyOff)
	} else if note == "..." {
		return playerNote(0)
	}

	ni := 0
	for ni = range notes {
		if notes[ni] == note[0:2] {
			break
		}
	}

	oct := int(note[2] - '2')
	return playerNote(12 + 12*oct + ni)
}

func decodeInt(sample string, replacement int) int {
	if sample == "" || sample == ".." {
		return replacement
	}

	ival, err := strconv.Atoi(sample)
	if err != nil {
		panic(err)
	}

	return ival
}

func decodeEffect(effect string) (byte, byte) {
	if effect == "" || effect == "..." {
		return 0, 0
	}

	param, err := strconv.ParseInt(effect[1:3], 16, 16)
	if err != nil {
		panic(err)
	}
	return convertS3MEffect(effect[0]-'A'+1, byte(param))
}

func validateChan(c *channel, sample, period, volume int, t *testing.T) {
	if c.sample != sample {
		t.Errorf("Expecting sample %d, got %d", sample, c.sample)
	}
	if c.period != period {
		t.Errorf("Expected period %d, got %d", period, c.period)
	}
	if c.volume != volume {
		t.Errorf("Expected volume %d, got %d", volume, c.volume)
	}
}

func validateChanToPlay(c *channel, sample, period, volume int, t *testing.T) {
	if c.sampleToPlay != sample {
		t.Errorf("Expected sample %d to be queued up, got %d", sample, c.sampleToPlay)
	}
	if c.periodToPlay != period {
		t.Errorf("Expected period %d to be queued up, got %d", period, c.periodToPlay)
	}
	if c.volumeToPlay != volume {
		t.Errorf("Expected volume %d to be queued, got %d", volume, c.volumeToPlay)
	}
}
