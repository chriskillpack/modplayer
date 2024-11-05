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
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. SD1"},
	}, t)
	plr.sequenceTick()

	// Nothing should be playing but the new note should be queued up
	c := &plr.channels[0]
	validateChan(c, -1, 0, 0, t)
	validateChanToPlay(c, 0, periodA4, 60, t)

	// Tick the player, note delay should have expired
	plr.sequenceTick()
	validateChan(c, 0, periodA4, 60, t)
}

func TestTriggerNDVolOnly(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: start a note playing
		{"... .. 15 SD1"}, // change volume with note delay
		{"... .. .. ..."}, // empty row so we can advance to it
	}, t)
	plr.sequenceTick()

	// Note should be playing
	c := &plr.channels[0]
	validateChan(c, 0, periodA4, 60, t)

	// On next row the note should continue to be playing with the same
	// volume, and then volume change should be queued up.
	advanceToNextRow(plr)
	validateChan(c, 0, periodA4, 60, t)
	validateChanToPlay(c, 0, periodA4, 15, t)

	// After the note delay expires the channel should have the new volume
	plr.sequenceTick()
	validateChan(c, 0, periodA4, 15, t)
}

func TestNoteOff(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: play a note
		{"^^. .. .. ..."}, // key off
	}, t)
	plr.sequenceTick()

	// Note should be playing
	c := &plr.channels[0]
	validateChan(c, 0, periodA4, 60, t)

	// Advance to second row and the note off effect
	advanceToNextRow(plr)
	validateChan(c, 0, 0, 0, t)
}

// Tests a specific bug: the note trigger logic rewrite incorrectly treated
// note portamentos as note delays, so it queued up changes (such as volume)
// that were never applied.
func TestNotePortamentoVol(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{
		{"A-4  1 .. ..."}, // setup: start playing a note
		{"B-4 .. 15 G05"}, // bug: active portamento should set volume
	}, t)
	plr.sequenceTick()

	// Note should be playing
	c := &plr.channels[0]
	validateChan(c, 0, periodA4, 60, t)

	// Advance to next row
	advanceToNextRow(plr)

	// Verify that the volume was applied
	validateChan(c, 0, periodA4, 15, t)

	// One more tick to verify that the portamento is happening
	plr.sequenceTick()
	validateChan(c, 0, 4048, 15, t) // period has shifted towards B-4 a little
}

func TestEffectSetSpeed(t *testing.T) {
	plr := newPlayerWithTestPattern([][]string{{"... .. .. A04"}}, t)
	if plr.Speed != 2 {
		t.Errorf("Expected initial speed of 2, got %d", plr.Speed)
	}
	plr.sequenceTick()
	if plr.Speed != 4 {
		t.Errorf("Expected initial speed of 4, got %d", plr.Speed)
	}
}

func TestEffectPatternJump(t *testing.T) {
	t.Skip("TODO")
}

func TestEffectPatternBreak(t *testing.T) {
	t.Skip("TODO")
}

func TestEffectVolumeSlide(t *testing.T) {
	cases := []struct {
		Name    string
		Notes   [][]string
		Volumes []int
	}{
		{"Slide down", [][]string{{"A-4  1 .. D01"}}, []int{60, 59, 58, 57, 56, 55}},
		{"Slide down x2", [][]string{{"A-4  1 .. D02"}}, []int{60, 58, 56, 54, 52, 50}},
		{"Slide up", [][]string{{"A-4  1 01 D10"}}, []int{1, 2, 3, 4, 5, 6}},
		{"Slide up x2", [][]string{{"A-4  1 01 D20"}}, []int{1, 3, 5, 7, 9, 11}},
		{"Fine slide down", [][]string{{"A-4  1 .. DF1"}}, []int{59, 59, 59, 59, 59, 59}},
		{"Fine slide up", [][]string{{"A-4  1 05 D1F"}}, []int{6, 6, 6, 6, 6, 6}},
		{"Memory", [][]string{{"A-4  1 .. D01"}, {"... .. .. D00"}}, []int{60, 59, 58, 57, 56, 55, 55, 54, 53, 52, 51, 50}},
		{"Memory fine slide", [][]string{{"A-4  1 .. DF1"}, {"... .. .. D00"}}, []int{59, 59, 59, 59, 59, 59, 58, 58, 58, 58, 58, 58}},
		// TODO - fast volume slides, not supported yet
	}
	const speed = 6
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			plr := newPlayerWithTestPattern(tc.Notes, t)
			plr.setSpeed(speed)

			c := &plr.channels[0]

			nrows := len(tc.Notes)
			for i := 0; i < speed*nrows; i++ {
				plr.sequenceTick()
				if c.volume != tc.Volumes[i] {
					t.Errorf("On tick %d expected volume %d, got %d", i, tc.Volumes[i], c.volume)
				}
			}
		})
	}
}

func TestEffectPortamento(t *testing.T) {
	cases := []struct {
		Name    string
		Notes   [][]string
		Periods []int
	}{
		{"Slide down", [][]string{{"A-4  1 .. E01"}}, []int{periodA4, periodA4 + 1*4, periodA4 + 2*4, periodA4 + 3*4, periodA4 + 4*4, periodA4 + 5*4}},
		{"Slide down x20", [][]string{{"A-4  1 .. E20"}}, []int{periodA4, periodA4 + 1*32*4, periodA4 + 2*32*4, periodA4 + 3*32*4, periodA4 + 4*32*4, periodA4 + 5*32*4}},
		{"Fine slide down", [][]string{{"A-4  1 .. EF7"}}, []int{periodA4 + 7*4, periodA4 + 7*4, periodA4 + 7*4, periodA4 + 7*4, periodA4 + 7*4, periodA4 + 7*4}},
		{"Extra fine slide down", [][]string{{"A-4  1 .. EE1"}}, []int{periodA4 + 1, periodA4 + 1, periodA4 + 1, periodA4 + 1, periodA4 + 1, periodA4 + 1}},
		{"Memory slide down", [][]string{{"A-4  1 .. E01"}, {"... .. .. E00"}}, []int{periodA4, periodA4 + 1*4, periodA4 + 2*4, periodA4 + 3*4, periodA4 + 4*4, periodA4 + 5*4, periodA4 + 5*4, periodA4 + 6*4, periodA4 + 7*4, periodA4 + 8*4, periodA4 + 9*4, periodA4 + 10*4}},
		{"Memory fine slide down", [][]string{{"A-4  1 .. EF1"}, {"... .. .. E00"}}, []int{periodA4 + 1*4, periodA4 + 1*4, periodA4 + 1*4, periodA4 + 1*4, periodA4 + 1*4, periodA4 + 1*4, periodA4 + 2*4, periodA4 + 2*4, periodA4 + 2*4, periodA4 + 2*4, periodA4 + 2*4, periodA4 + 2*4}},

		{"Slide up", [][]string{{"A-4  1 .. F01"}}, []int{periodA4, periodA4 - 1*4, periodA4 - 2*4, periodA4 - 3*4, periodA4 - 4*4, periodA4 - 5*4}},
		{"Slide up x20", [][]string{{"A-4  1 .. F20"}}, []int{periodA4, periodA4 - 1*32*4, periodA4 - 2*32*4, periodA4 - 3*32*4, periodA4 - 4*32*4, periodA4 - 5*32*4}},
		{"Fine slide up", [][]string{{"A-4  1 .. FF7"}}, []int{periodA4 - 7*4, periodA4 - 7*4, periodA4 - 7*4, periodA4 - 7*4, periodA4 - 7*4, periodA4 - 7*4}},
		{"Extra fine slide up", [][]string{{"A-4  1 .. FE1"}}, []int{periodA4 - 1, periodA4 - 1, periodA4 - 1, periodA4 - 1, periodA4 - 1, periodA4 - 1}},
		{"Memory slide up", [][]string{{"A-4  1 .. F01"}, {"... .. .. F00"}}, []int{periodA4, periodA4 - 1*4, periodA4 - 2*4, periodA4 - 3*4, periodA4 - 4*4, periodA4 - 5*4, periodA4 - 5*4, periodA4 - 6*4, periodA4 - 7*4, periodA4 - 8*4, periodA4 - 9*4, periodA4 - 10*4}},
		{"Memory fine slide up", [][]string{{"A-4  1 .. FF1"}, {"... .. .. F00"}}, []int{periodA4 - 1*4, periodA4 - 1*4, periodA4 - 1*4, periodA4 - 1*4, periodA4 - 1*4, periodA4 - 1*4, periodA4 - 2*4, periodA4 - 2*4, periodA4 - 2*4, periodA4 - 2*4, periodA4 - 2*4, periodA4 - 2*4}},
	}
	const speed = 6
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			plr := newPlayerWithTestPattern(tc.Notes, t)
			plr.setSpeed(speed)

			nrows := len(tc.Notes)

			c := &plr.channels[0]
			for i := 0; i < speed*nrows; i++ {
				plr.sequenceTick()
				if c.period != tc.Periods[i] {
					t.Errorf("On tick %d, expected a period of %d, got %d", i, tc.Periods[i], c.period)
				}
			}
		})
	}
}

func TestEffectTonePortamento(t *testing.T) {
	cases := []struct {
		Name    string
		Notes   [][]string
		Periods []int
	}{
		{"Portamento up", [][]string{{"A-4  1 .. ..."}, {"B-4 .. .. G10"}, {"... .. .. G00"}},
			[]int{periodA4, 4004, 3940, 3876, 3812, 3748, 3748, 3684, periodB4, periodB4, periodB4, periodB4}},
		{"Portamento down", [][]string{{"B-4  1 .. ..."}, {"A-4 .. .. G10"}, {"... .. .. G00"}},
			[]int{periodB4, 3688, 3752, 3816, 3880, 3944, 3944, 4008, periodA4, periodA4, periodA4, periodA4}},
	}
	const speed = 6
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			plr := newPlayerWithTestPattern(tc.Notes, t)
			plr.setSpeed(speed)

			nrows := len(tc.Notes)

			c := &plr.channels[0]
			for i := 0; i < speed*nrows; i++ {
				plr.sequenceTick()
				if i > speed {
					if c.period != tc.Periods[i-speed] {
						t.Errorf("On tick %d expected period %d, got %d", i, tc.Periods[i-speed], c.period)
					}
				}
			}
		})
	}
}

func TestEffectVibrato(t *testing.T) {
	cases := []struct {
		Name        string
		Notes       [][]string
		Adjustments []int
	}{
		{"No vibrato", [][]string{{"A-4  1 .. ..."}}, []int{0, 0, 0, 0, 0, 0}},

		{"Sine wave no depth", [][]string{{"... .. .. S30"}, {"A-4  1 .. H10"}}, []int{0, 0, 0, 0, 0, 0}},
		// TODO - investigate why the vibrato adjust goes to 0 at the start of a new row
		// Is this correct behavior or a bug? True for all the vibrato tests below.
		{"Sine wave", [][]string{{"... .. .. S30"}, {"A-4  1 .. H2A"}, {"... .. .. H00"}}, []int{0, 0, 3, 7, 11, 14, 0, 16, 18, 19, 19, 19}},
		{"Faster sine wave", [][]string{{"... .. .. S30"}, {"A-4  1 .. H4A"}, {"... .. .. H00"}}, []int{0, 0, 7, 14, 18, 19, 0, 18, 14, 7, 0, -8}},

		{"Ramp down", [][]string{{"... .. .. S31"}, {"A-4  1 .. H2A"}, {"... .. .. H00"}}, []int{0, -20, -19, -18, -16, -15, 0, -14, -13, -11, -10, -9}},
		{"Ramp down faster", [][]string{{"... .. .. S31"}, {"A-4  1 .. H4A"}, {"... .. .. H00"}}, []int{0, -20, -18, -15, -13, -10, 0, -8, -5, -3, 0, 2}},

		{"Square wave", [][]string{{"... .. .. S32"}, {"A-4  1 .. H6A"}, {"... .. .. H00"}}, []int{0, 19, 19, 19, 19, 19, 0, 19, 0, 0, 0, 0}},
	}

	const speed = 6
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			plr := newPlayerWithTestPattern(tc.Notes, t)
			plr.setSpeed(speed)

			c := &plr.channels[0]

			nrows := len(tc.Notes)
			for i := 0; i < speed*nrows; i++ {
				plr.sequenceTick()
				if i >= speed && c.vibratoAdjust != tc.Adjustments[i-speed] {
					t.Errorf("On tick %d expected vibrato adjustment %d, got %d", i, tc.Adjustments[i-speed], c.vibratoAdjust)
				}
			}
		})
	}
}

func TestEffectRetrig(t *testing.T) {
	type trigger struct {
		Tick, Volume int
	}
	cases := []struct {
		Name     string
		Notes    [][]string
		Triggers []trigger
	}{
		// MOD style retrigs, no change in volume
		{"Retrig every tick", [][]string{{"A-4  1 .. Q01"}}, []trigger{{0, 60}, {1, 60}, {2, 60}, {3, 60}, {4, 60}, {5, 60}}},
		{"Retrig three times", [][]string{{"A-4  1 .. Q02"}}, []trigger{{0, 60}, {2, 60}, {4, 60}}},
		{"Retrig twice", [][]string{{"A-4  1 .. Q03"}}, []trigger{{0, 60}, {3, 60}}},
		{"No retrig", [][]string{{"A-4  1 .. Q07"}}, []trigger{{0, 60}}},

		// S3M style retrigs, volume slides and retrigs
		{"Volume -1", [][]string{{"A-4  1 .. Q11"}}, []trigger{{0, 60}, {1, 59}, {2, 58}, {3, 57}, {4, 56}, {5, 55}}},
		{"Volume -2", [][]string{{"A-4  1 .. Q21"}}, []trigger{{0, 60}, {1, 58}, {2, 56}, {3, 54}, {4, 52}, {5, 50}}},
		{"Volume -4", [][]string{{"A-4  1 .. Q31"}}, []trigger{{0, 60}, {1, 56}, {2, 52}, {3, 48}, {4, 44}, {5, 40}}},
		{"Volume -8", [][]string{{"A-4  1 .. Q41"}}, []trigger{{0, 60}, {1, 52}, {2, 44}, {3, 36}, {4, 28}, {5, 20}}},
		{"Volume -16", [][]string{{"A-4  1 .. Q51"}}, []trigger{{0, 60}, {1, 44}, {2, 28}, {3, 12}, {4, 0}, {5, 0}}},
		{"Volume +1", [][]string{{"A-4  1 00 Q91"}}, []trigger{{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 4}, {5, 5}}},
		{"Volume +2", [][]string{{"A-4  1 00 QA1"}}, []trigger{{0, 0}, {1, 2}, {2, 4}, {3, 6}, {4, 8}, {5, 10}}},
		{"Volume +4", [][]string{{"A-4  1 00 QB1"}}, []trigger{{0, 0}, {1, 4}, {2, 8}, {3, 12}, {4, 16}, {5, 20}}},
		{"Volume +8", [][]string{{"A-4  1 00 QC1"}}, []trigger{{0, 0}, {1, 8}, {2, 16}, {3, 24}, {4, 32}, {5, 40}}},
		{"Volume +16", [][]string{{"A-4  1 00 QD1"}}, []trigger{{0, 0}, {1, 16}, {2, 32}, {3, 48}, {4, 64}, {5, 64}}},
		{"Volume *2/3", [][]string{{"A-4  1 .. Q61"}}, []trigger{{0, 60}, {1, 40}, {2, 26}, {3, 17}, {4, 11}, {5, 7}}},
		{"Volume *1/2", [][]string{{"A-4  1 .. Q71"}}, []trigger{{0, 60}, {1, 30}, {2, 15}, {3, 7}, {4, 3}, {5, 1}}},
		{"Volume *3/2", [][]string{{"A-4  1 2 QE1"}}, []trigger{{0, 2}, {1, 3}, {2, 4}, {3, 6}, {4, 9}, {5, 13}}},
		{"Volume *2/1", [][]string{{"A-4  1 1 QF1"}}, []trigger{{0, 1}, {1, 2}, {2, 4}, {3, 8}, {4, 16}, {5, 32}}},

		// No-op
		{"Volume no-op", [][]string{{"A-4  1 .. Q83"}}, []trigger{{0, 60}, {3, 60}}},

		// Memory
		{"Memory no vol slide", [][]string{{"A-4  1 .. Q03"}, {"... .. .. Q00"}}, []trigger{{0, 60}, {3, 60}, {0, 60}, {3, 60}}},
		{"Memory vol slide", [][]string{{"A-4  1 10 QF3"}, {"... .. .. Q00"}}, []trigger{{0, 10}, {3, 20}, {0, 40}, {3, 64}}},
	}
	for _, tc := range cases {
		const speed = 6

		t.Run(tc.Name, func(t *testing.T) {
			plr := newPlayerWithTestPattern(tc.Notes, t)
			plr.setSpeed(speed)

			c := &plr.channels[0]

			nrows := len(tc.Notes)

			tick := -1
			triggers := []trigger{}
			for i := 0; i < speed*nrows; i++ {
				plr.sequenceTick()
				if c.trigTick != tick {
					triggers = append(triggers, trigger{c.trigTick, c.volume})
					tick = c.trigTick
				}
			}

			if len(triggers) != len(tc.Triggers) {
				t.Errorf("Expected %d triggers got %d", len(tc.Triggers), len(triggers))
			}

			for i, trig := range tc.Triggers {
				if triggers[i].Tick != trig.Tick {
					t.Errorf("Trigger %d happened on tick %d instead of %d", i, triggers[i].Tick, trig.Tick)
				}
				if triggers[i].Volume != trig.Volume {
					t.Errorf("Trigger %d has volume %d expected %d", i, triggers[i].Volume, trig.Volume)
				}
			}
		})
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
