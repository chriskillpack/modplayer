package modplayer

import (
	"bytes"
	"os"
	"testing"
)

var mixBuffer = make([]int16, 10*1024*2)

const (
	periodA4 = 4068
	periodB4 = 3624
	periodC3 = 13696
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
	if player.row != -1 {
		t.Errorf("Expected player on row -1, got %d\n", player.row)
	}

	for i := 0; i < player.Song.Channels; i++ {
		c := &player.channels[i]

		validateChan(c, -1, 0, 0, t)
		validateChanToPlay(c, -1, 0, 0, t)

		if c.pan != int(player.Song.pan[i]) {
			t.Errorf("Expected channel %d to have pan %d, got %d\n", i, player.Song.pan[i], c.pan)
		}
	}
}

func TestTwoChannels(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4 1 33 ...", "C-3 1 .. S12"},
	}, t)
	// Run one tick of the player
	plr.sequenceTick()

	validateChan(&plr.channels[0], 0, periodA4, 33, t)
	validateChan(&plr.channels[1], 0, periodC3, 60, t)
}

func TestTriggerJustNoteNoPriorInstrument(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		// With no prior instrument
		{"A-4 .. .. ..."},
	}, t)
	plr.sequenceTick()

	validateChan(&plr.channels[0], -1, 0, 0, t)
}

func TestTriggerNoteOnly(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: assign an instrument to the channel
		{"B-4 .. .. ..."}, // test: play the new note with the existing instrument
	}, t)
	plr.sequenceTick()
	advanceToNextRow(plr)

	validateChan(&plr.channels[0], 0, periodB4, 60, t)
}

func TestTriggerInsOnlyDiffIns(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: start a note playing
		{"...  2 .. ..."}, // instrument only should stop currently playing instrument as well
	}, t)
	plr.GenerateAudio(mixBuffer[0 : (2*plr.samplesPerTick-1)*2]) // advance to 1 sample before end of second tick

	c := &plr.channels[0]
	if c.sampleToPlay != 0 {
		t.Errorf("Expected next note to use sample 1, got %d", c.sampleToPlay)
	}

	if c.samplePosition == 0 {
		t.Error("Expected progress to have been made through sample")
	}

	plr.GenerateAudio(mixBuffer[0 : 2*2]) // advance to beginning of second row
	if plr.row != 1 {
		t.Error("Player did not advance to second row")
	}
	if c.sampleToPlay != 1 || c.sample != -1 {
		t.Errorf("Channel configuration was wrong, c.stp %d c.s %d", c.sampleToPlay, c.sample)
	}
}

func TestTriggerInsOnlySameIns(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 15 ..."}, // setup: start a note playing
		{"...  1 .. ..."}, // instrument only, continue playing original note at instrument default volume
	}, t)
	plr.GenerateAudio(mixBuffer[0 : (2*plr.samplesPerTick-1)*2]) // advance to 1 sample before end of second tick

	c := &plr.channels[0]
	validateChan(c, 0, periodA4, 15, t)

	plr.GenerateAudio(mixBuffer[0 : 2*2]) // advance to beginning of second row
	validateChan(c, 0, periodA4, 60, t)
}

func TestTriggerNoteInstrument(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4 1 .. ..."}, // setup: assign an instrument to the channel
	}, t)
	plr.sequenceTick()

	validateChan(&plr.channels[0], 0, periodA4, 60, t)
}

func TestTriggerVolumeOnly(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: assign an instrument to the channel
		{"... .. 23 ..."}, // test: change channel volume
	}, t)
	plr.sequenceTick()

	// Setup - make sure that the channel has a volume on it
	c := &plr.channels[0]
	validateChan(c, 0, periodA4, 60, t)

	advanceToNextRow(plr)

	validateChan(c, 0, periodA4, 23, t)
}

func TestTriggerNoteAndVolume(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: assign an instrument to the channel
		{"B-4 .. 23 ..."}, // test: change channel volume
	}, t)
	plr.sequenceTick()
	advanceToNextRow(plr)

	validateChan(&plr.channels[0], 0, periodB4, 23, t)
}

func TestTriggerInsAndVolume(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: assign an instrument to the channel
		{"...  2 20 ..."}, // next note should use instrument 2 and volume 20
		{"B-4 .. .. ..."}, // final check that B-4 plays with sample 2
	}, t)
	plr.sequenceTick() // process the first row

	// Advance to second row and verify that sample 2 will be used for the next
	// note.
	advanceToNextRow(plr)
	c := &plr.channels[0]
	if c.sampleToPlay != 1 {
		t.Error("Expecting sample 2 to be set on channel", c.sample)
	}

	// Advance to third row and verify that the played note is using sample 2.
	advanceToNextRow(plr)
	validateChan(c, 1, periodB4, 20, t)
}

func TestTriggerNoteVolumeInstrument(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1  20 ..."},
	}, t)
	plr.sequenceTick()

	validateChan(&plr.channels[0], 0, periodA4, 20, t)
}

func TestTriggerNDNote(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"B-4  1 .. ..."}, // setup: channel already playing a note
		{"A-4 .. .. SD1"}, // line under test
		{"... .. .. ..."}, // empty line to allow player advance
	}, t)
	plr.sequenceTick()

	// Note should be playing with default volume
	c := &plr.channels[0]
	validateChan(c, 0, periodB4, 60, t)

	// Advance to second row
	advanceToNextRow(plr)

	// The A-4 note has a note delay on it which hasn't expired yet so the B-4
	// should still be playing
	validateChan(c, 0, periodB4, 60, t)

	// Next, check that the A-4 is queued up and ready to play
	validateChanToPlay(c, 0, periodA4, 60, t)

	// Finally run the player forward until note delay has elapsed and check
	// that the delayed note is now playing
	advanceToNextRow(plr)
	validateChan(c, 0, periodA4, 60, t)
}

func TestTriggerNDNoteIns(t *testing.T) {
	t.Skip("Incomplete")

	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. SD1"},
	}, t)
	_ = plr
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
