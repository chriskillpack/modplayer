package modplayer

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestLoadMODSong(t *testing.T) {
	mod, err := os.ReadFile("mods/space_debris.mod")
	if err != nil {
		t.Fatal(err)
	}
	song, err := NewMODSongFromBytes(mod)
	if err != nil {
		t.Fatal(err)
	}

	if song.Title != "space_debris" {
		t.Errorf("Incorrect song title %s", song.Title)
	}
	if song.Channels != 4 {
		t.Errorf("Expecting 4 channels, got %d", song.Channels)
	}
	if len(song.Orders) != 42 {
		t.Errorf("Expecting 42 orders, got %d", len(song.Orders))
	}
	if !bytes.Equal(song.Orders[0:3], []byte{1, 2, 3}) || song.Orders[41] != 0x28 {
		t.Errorf("Order data is wrong")
	}
}

func TestNoteDataFor(t *testing.T) {
	player, err := newTestPlayerFromMod("testdata/notes.mod")
	if err != nil {
		t.Fatal(err)
	}

	if player.Song.Channels != 4 {
		t.Errorf("expected 4 channel MOD, got %d", player.Song.Channels)
	}

	type testnote struct {
		note       string
		instrument int
	}
	expected := []struct {
		row   int
		notes []testnote
	}{
		{0, []testnote{
			{"C-4", 1},
			{"C#4", 2},
			{"D-4", 3},
			{"D#4", 4},
		}},
		{1, []testnote{
			{"D-5", 1},
			{"D#5", 2},
			{"G-5", 3},
			{"G#5", 4},
		}},
		{2, []testnote{
			{"C-6", 1},
			{"C#6", 2},
			{"D-6", 3},
			{"E-6", 4},
		}},
	}
	for _, ex := range expected {
		ndf := player.NoteDataFor(0, ex.row)
		for i, nd := range ndf {
			if ex.notes[i].instrument != nd.Instrument {
				t.Errorf("Note %d of row %d, expected instrument %d actual %d", i, ex.row, ex.notes[i].instrument, nd.Instrument)
			}
			if ex.notes[i].note != nd.Note {
				t.Errorf("Note %d of row %d, expected note %s actual %s", i, ex.row, ex.notes[i].note, nd.Note)
			}
		}
	}
}

func TestPlayerInitialState(t *testing.T) {
	player, err := newTestPlayerFromMod("testdata/mix.mod")
	if err != nil {
		t.Fatal(err)
	}

	if player.order != 0 {
		t.Errorf("Expected player on order 0, got %d\n", player.order)
	}
	if player.row != 0 {
		t.Errorf("Expected player on row 0, got %d\n", player.row)
	}

	for i := 0; i < player.Song.Channels; i++ {
		c := &player.channels[i]
		if c.sample != -1 {
			t.Errorf("Expected channel %d to have sample -1, got %d\n", i, c.sample)
		}
		if c.period != 0 {
			t.Errorf("Expected channel %d to have period 0, got %d\n", i, c.period)
		}
		if c.volume != 0 {
			t.Errorf("Expected channel %d to have volume 0, got %d\n", i, c.volume)
		}
		if c.pan != int(player.Song.pan[i]) {
			t.Errorf("Expected channel %d to have pan %d, got %d\n", i, player.Song.pan[i], c.pan)
		}
	}
}

func TestTwoChannels(t *testing.T) {
	player, err := newPlayerWithTestPattern([][]string{
		{"A-4 1 33 ...", "C#3 1 .. S12"},
	})
	if err != nil {
		t.Fatalf("Could not create test player: %e", err)
	}
	// Run one tick of the player
	player.sequenceTick()

	if player.channels[0].sample != 0 {
		t.Errorf("Expected channel to be playing sample 0")
	}
	if player.channels[0].volume != 33 {
		t.Errorf("Channel has incorrect volume")
	}
	if player.channels[0].period != 4068 {
		t.Errorf("expected channel to have period 4068, got %d", player.channels[0].period)
	}
	if player.channels[1].sample != 0 {
		t.Errorf("Expected channel to be playing sample 0")
	}
	if player.channels[1].volume != 63 {
		t.Errorf("Channel has incorrect volume")
	}
	if player.channels[1].period != 12924 {
		t.Errorf("expected channel to have period 4068, got %d", player.channels[1].period)
	}
}

func TestTriggerJustNoteNoPriorInstrument(t *testing.T) {
	plr, err := newPlayerWithTestPattern([][]string{
		// With no prior instrument
		{"A-4 .. .. ..."},
	})
	if err != nil {
		t.Fatalf("Could not create test player: %e", err)
	}
	// Run one tick of the player
	plr.sequenceTick()

	if plr.channels[0].sample != -1 {
		t.Errorf("Expected no sample")
	}
}

func TestTriggerJustNote(t *testing.T) {
	plr, err := newPlayerWithTestPattern([][]string{
		{"A-4 1 .. ..."}, // setup, an instrument was setup
		{"B-4 .. .. ..."},
	})
	if err != nil {
		t.Fatalf("Could not create test player: %e", err)
	}
	// Run one tick of the player
	plr.sequenceTick()
	plr.sequenceTick()
	plr.sequenceTick()

	if plr.channels[0].period != 3624 {
		t.Errorf("Expected period of 3624")
	}
	if plr.channels[0].sample != 0 {
		t.Errorf("Expected no sample")
	}
}

func BenchmarkMixChannels(b *testing.B) {
	player, err := newTestPlayerFromMod("testdata/mix.mod")
	if err != nil {
		b.Fatal(err)
	}

	out := make([]int16, 1024*2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		player.GenerateAudio(out) // internally this calls MixChannels
	}
}

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

func newPlayerWithTestPattern(pattern [][]string) (*Player, error) {
	noteData, nChannels := convertTestPatternData(pattern)

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
				Volume:  63,
				C4Speed: 8363,
			},
		},
		patterns: noteData,
	}
	player, err := NewPlayer(song, 44100)
	if err != nil {
		return nil, err
	}
	player.Start()
	return player, err
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

	param, err := strconv.ParseInt(effect[1:3], 16, 8)
	if err != nil {
		panic(err)
	}
	return convertS3MEffect(effect[0]-'A', byte(param))
}
