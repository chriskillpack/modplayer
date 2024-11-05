package modplayer

// Something else to look at https://www.celersms.com/doc/XM_file_format.pdf

import (
	"fmt"
	"io"
	"math"
)

const (
	retracePALHz = 14187578.4 // Amiga PAL vertical retrace timing

	rowsPerPattern = 64
	noteKeyOff     = 254
	maxVolume      = 64   // channel maximum volume
	mixBufferLen   = 8192 // samples per channel
	noNoteVolume   = 255  // note data does not have a volume set

	// MOD note effects
	effectPortamentoUp        = 0x1
	effectPortamentoDown      = 0x2
	effectPortaToNote         = 0x3
	effectVibrato             = 0x4 // TODO: Complete
	effectPortaToNoteVolSlide = 0x5
	effectTremolo             = 0x7 // TODO: Complete
	effectSetPanPosition      = 0x8
	effectSampleOffset        = 0x9
	effectVolumeSlide         = 0xA
	effectJumpToPattern       = 0xB
	effectSetVolume           = 0xC
	effectPatternBrk          = 0xD
	effectExtended            = 0xE
	effectSetSpeed            = 0xF

	// Internal effects
	effectPatternLoop        = 0x20
	effectS3MVolumeSlide     = 0x21
	effectS3MPortamentoDown  = 0x22
	effectS3MPortamentoUp    = 0x23
	effectS3MGlobalVolume    = 0x24
	effectNoteRetrigVolSlide = 0x25

	// Extended effects (Exy), x = effect, y effect param
	effectExtendedVibratoWaveform  = 0x4
	effectExtendedNoteRetrig       = 0x9 // Gets converted to effectNoteRetrigVolSlide in the MOD loader
	effectExtendedFineVolSlideUp   = 0xA
	effectExtendedFineVolSlideDown = 0xB
	effectExtendedNoteCut          = 0xC
	effectExtendedNoteDelay        = 0xD
)

// Player can play a MOD file. It must be initialized with a Song,
// see NewPlayer().
type Player struct {
	*Song
	samplingFrequency uint
	globalVolume      uint
	volBoost          uint

	// song configuration
	Tempo          int
	Speed          int
	samplesPerTick int

	// These next fields track player position in the song
	tickSamplePos int // the number of samples in the tick
	tick          int // decrementing counter for number of ticks per row
	row           int // which row in the order
	order         int // current order of the song
	ordersplayed  int // number of orders played
	playing       bool

	// Bitmask of muted channels, channel 1 in LSB. To mute a channel set
	// its bit to 1.
	Mute uint

	PlayOrderLimit int // maximum number of orders to play, -1 to disable limit

	loop     []loopinfo
	channels []channel

	// Internal buffer the audio is mixed into. This is done to allow loud
	// sounds without clipping.
	mixbuffer []int
}

// ChannelNoteData represents the note data for a channel
type ChannelNoteData struct {
	Note       string // 'A-4', 'C#3', ...
	Instrument int    // -1 if no instrument
	Volume     int    // 0xFF = not set ignore
	Effect     int
	Param      int
}

// String returns a formatted string of the note data
func (c *ChannelNoteData) String() string {
	return fmt.Sprintf("%s %2X %2X %X%02X", c.Note, c.Instrument, c.Volume, c.Effect, c.Param)
}

// ChannelState holds the current state of a channel
type ChannelState struct {
	Instrument         int // -1 if no instrument playing
	TrigOrder, TrigRow int // The order and row the instrument was triggered (played)
}

// PlayerState holds player position and channel state
type PlayerState struct {
	Order   int
	Pattern int
	Row     int

	Notes    []ChannelNoteData
	Channels []ChannelState
}

// playerNote defines a note pitch as octave*12+semitone
// There are 12 semitones in an octave. This encoding is very similar to how
// MIDI defines pitch values.
type playerNote int

// String returns the note pitch in name-octave form, e.g. C-4, A#2.
// Returns three spaces if the note is not recognized.
func (note playerNote) String() string {
	switch note {
	case 0:
		return "   "
	case noteKeyOff:
		return "^^."
	default:
		return fmt.Sprintf("%s%d", notes[note%12], note/12-1)
	}
}

type vibType int

const (
	vibratoSine vibType = iota
	vibratoRampDown
	vibratoSquareWave
)

// Internal representation of a pattern note
type note struct {
	Sample int
	Pitch  playerNote
	Volume int // Unused by MOD files, FF=no value set, ignore
	Effect byte
	Param  byte
}

type channel struct {
	sample         int // sample that is being played (or -1 if no sample)
	sampleToPlay   int // sample _to be played_, used for Note Delay effect
	period         int
	periodToPlay   int // period of a note with note delay
	portaPeriod    int // Portamento destination as a period
	portaSpeed     int
	volume         int
	volumeToPlay   int // volume _to be played_, used for Note Delay effect
	pan            int // Pan position, 0=Full Left, 127=Full Right
	samplePosition uint

	tremoloDepth  int
	tremoloSpeed  int
	tremoloPhase  int
	tremoloAdjust int

	vibratoDepth    int
	vibratoSpeed    int
	vibratoPhase    int
	vibratoAdjust   int
	vibratoWaveform vibType

	effect        byte
	param         byte
	effectCounter int

	memVolSlide   byte // saved volume slide parameter
	memPortamento byte // saved portamento parameter (this is shared by the up and down commands)
	memRetrig     byte // saved retrig parameter

	// When the note was triggered
	trigOrder int
	trigRow   int
	trigTick  int
}

type loopinfo struct {
	start int
	count int
}

// Song represents a MOD or S3M file
type Song struct {
	Title        string
	Channels     int
	Orders       []byte
	Tempo        int // in beats per minute
	Speed        int // number of tempo ticks before advancing to the next row
	GlobalVolume int

	Samples  []Sample
	patterns [][]note
	pan      [32]byte
}

// Sample holds information about an instrument sample including sample data
type Sample struct {
	Name      string
	Length    int
	Volume    int
	LoopStart int
	LoopLen   int
	C4Speed   int
	Data      []int8
}

func (s Sample) String() string {
	return fmt.Sprintf(
		"\tName:\t\t%s\n"+
			"\tLength:\t\t%d\n"+
			"\tVolume:\t\t%d\n"+
			"\tLoop Start:\t%d\n"+
			"\tLoop Len:\t%d\n", s.Name, s.Length, s.Volume, s.LoopStart, s.LoopLen,
	)
}

var (
	// Amiga period values. This table is used to map the note period
	// in the MOD file to a note index for display. It is not used in
	// the mixer.
	//lint:ignore U1000 This will be reused later
	periodTable = []int{
		// C-2, C#2, D-2, ..., B-2
		1712, 1616, 1524, 1440, 1356, 1280, 1208, 1140, 1076, 1016, 960, 907,
		// C-3, C#3, D-3, ..., B-3
		856, 808, 762, 720, 678, 640, 604, 570, 538, 508, 480, 453,
		// C-4, C#4, D-4, ..., B-4
		428, 404, 381, 360, 339, 320, 302, 285, 269, 254, 240, 226,
		// C-5, C#5, D-5, ..., B-5
		214, 202, 190, 180, 170, 160, 151, 143, 135, 127, 120, 113,
		// C-6, C#6, D-6, ..., B-6
		107, 101, 95, 90, 85, 80, 75, 71, 67, 63, 60, 56,
	}

	//lint:ignore U1000 This will be reused later
	s3mPeriodTable = []int{
		// C-?, C#?, D-?, ..., B-?
		1712, 1616, 1524, 1440, 1356, 1280, 1208, 1140, 1076, 1016, 960, 907,
	}

	// ProTracker sine table. 32-elements representing the first half of the sine
	// period. The second half of the period has the same magnitude but with the
	// sign flipped: 0, -24, -49, ... To use the sine table:
	// IF phase >= 32 THEN sineTable[phase & 31]
	//                ELSE -sineTable[phase & 31]
	// phase = phase & 63
	sineTable = []int{
		0, 24, 49, 74, 97, 120, 141, 161, 180, 197, 212, 224, 235, 244, 250, 253,
		255, 253, 250, 244, 235, 224, 212, 197, 180, 161, 141, 120, 97, 74, 49, 24,
	}

	// Literal notes
	notes = []string{
		"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-",
	}

	dumpW io.Writer = nil
)

func (c *channel) portaToNote() {
	period := c.period
	if period < c.portaPeriod {
		period += c.portaSpeed * 4
		if period > c.portaPeriod {
			period = c.portaPeriod
		}
	} else if period > c.portaPeriod {
		period -= c.portaSpeed * 4
		if period < c.portaPeriod {
			period = c.portaPeriod
		}
	}
	c.period = period
}

func (c *channel) volumeSlide() {
	vol := c.volume
	if (c.param >> 4) > 0 {
		vol += int(c.param >> 4)
		if vol > maxVolume {
			vol = maxVolume
		}
	} else if c.param != 0 {
		vol -= int(c.param & 0xF)
		if vol < 0 {
			vol = 0
		}
	}
	c.volume = vol
}

func SetDumpWriter(w io.Writer) { dumpW = w }

// NewPlayer returns a new Player for the given song. The Player is already
// started.
func NewPlayer(song *Song, samplingFrequency uint) (*Player, error) {
	player := &Player{
		samplingFrequency: samplingFrequency,
		volBoost:          1,
		globalVolume:      uint(song.GlobalVolume),
		Song:              song,
		Speed:             6,
		PlayOrderLimit:    -1,
	}

	player.loop = make([]loopinfo, song.Channels)
	player.channels = make([]channel, song.Channels)
	player.mixbuffer = make([]int, mixBufferLen*2)

	player.reset()
	player.Start()

	return player, nil
}

// Start tells the player to start playing. Calls to GenerateAudio will advance
// the song position and generate audio samples.
func (p *Player) Start() {
	p.playing = true
}

// Stop tells the player to stop playing. Calls to GenerateAudio will not
// advance the song position or generate audio samples. A stopped player
// preserves state and a subsequent call to Start carries on where the player
// left off.
func (p *Player) Stop() {
	p.playing = false
}

// IsPlaying returns if the song is being played
func (p *Player) IsPlaying() bool {
	return p.playing
}

// State returns the current state of the player (song position, channel state, etc.)
func (p *Player) State() PlayerState {
	rc := p.row
	if rc < 0 {
		rc = 0
	}
	state := PlayerState{Order: p.order, Pattern: int(p.Song.Orders[p.order]), Row: rc}
	state.Notes = make([]ChannelNoteData, p.Channels)
	state.Channels = make([]ChannelState, p.Channels)

	pattern := int(p.Song.Orders[p.order])
	rowDataIdx := p.rowDataIndex()

	for i := range state.Notes {
		patnote := &p.Song.patterns[pattern][rowDataIdx]

		note := &state.Notes[i]
		note.Note = patnote.Pitch.String()
		note.Instrument = patnote.Sample
		note.Volume = patnote.Volume
		note.Effect = int(patnote.Effect)
		note.Param = int(patnote.Param)

		rowDataIdx++
	}

	for i := range p.channels {
		state.Channels[i].Instrument = p.channels[i].sample
		if p.channels[i].sample != -1 {
			state.Channels[i].TrigOrder = p.channels[i].trigOrder
			state.Channels[i].TrigRow = p.channels[i].trigRow
		} else {
			state.Channels[i].TrigOrder = -1
			state.Channels[i].TrigRow = -1
		}
	}

	return state
}

// SeekTo sets the player's current position. If the position is off the end of
// the song then it will be set back to the beginning of the final order. No
// attempt is made to reset the player internals.
func (p *Player) SeekTo(order, row int) {
	if order < 0 {
		order = 0
	} else if order >= len(p.Orders) {
		order = len(p.Orders) - 1
	}
	p.order = order

	if row < 0 {
		row = 0
	} else if row >= 64 {
		row = 63
	}
	p.row = row - 1
	p.tick = p.Speed - 1
}

// SetVolumeBoost sets the volume boost factor to a value between 1 (no boost,
// default and 4 (4x volume).
func (p *Player) SetVolumeBoost(boost int) error {
	if boost < 1 || boost > 4 {
		return fmt.Errorf("invalid volume boost")
	}
	p.volBoost = uint(boost)

	return nil
}

// NoteDataFor returns the note data for a specific order and row, or nil if
// the requested position is invalid.
func (p *Player) NoteDataFor(order, row int) []ChannelNoteData {
	if order < 0 || row < 0 || order >= len(p.Orders) || row >= 64 {
		return nil
	}
	nd := make([]ChannelNoteData, p.Channels)

	pattern := p.Orders[order]
	rowDataIdx := row * p.Song.Channels
	for i := 0; i < p.Channels; i++ {
		patnote := &p.Song.patterns[pattern][rowDataIdx]

		note := &nd[i]
		note.Note = patnote.Pitch.String()
		note.Instrument = patnote.Sample
		note.Volume = patnote.Volume
		note.Effect = int(patnote.Effect)
		note.Param = int(patnote.Param)

		rowDataIdx++
	}

	return nd
}

func (p *Player) reset() {
	p.Stop()
	p.setTempo(p.Song.Tempo)
	p.Speed = p.Song.Speed
	p.order = 0

	// Setup counters so that the first "tick" of the player executes the
	// first row immediately.
	p.tick = p.Speed - 1
	p.row = -1
	p.tickSamplePos = p.samplesPerTick

	for i := 0; i < p.Song.Channels; i++ {
		channel := &p.channels[i]
		channel.sample = -1
		channel.sampleToPlay = -1
		channel.volume = 0
		channel.volumeToPlay = 0
		channel.period = 0
		channel.periodToPlay = 0
		channel.tremoloDepth = 0
		channel.tremoloSpeed = 0
		channel.tremoloPhase = 0
		channel.tremoloAdjust = 0
		channel.vibratoDepth = 0
		channel.vibratoSpeed = 0
		channel.vibratoPhase = 0
		channel.vibratoAdjust = 0
		channel.vibratoWaveform = vibratoSine
		channel.pan = int(p.Song.pan[i])
		channel.memVolSlide = 0
		channel.memPortamento = 0
		channel.memRetrig = 0
	}
}

func (p *Player) setTempo(tempo int) {
	// TODO: What to do if new samplesPerTick value is now < tickSamplePos?
	p.samplesPerTick = int((p.samplingFrequency<<1)+(p.samplingFrequency>>1)) / tempo
	p.Tempo = tempo
}

func (p *Player) setSpeed(speed int) {
	p.Speed = speed
	p.tick = p.Speed - 1 // TODO - is setting the tick like this appropriate?
}

func (p *Player) channelTick(c *channel, ci, tick int) {
	c.effectCounter++

	switch c.effect {
	case effectPortamentoUp:
		c.period -= int(c.param) * 4
		if c.period < 1 {
			c.period = 1
		}
	case effectPortamentoDown:
		c.period += int(c.param) * 4
		if c.period > 65535 {
			c.period = 65535
		}
	case effectPortaToNote:
		c.portaToNote()
	case effectVibrato:
		c.vibratoAdjust = (vibratoFn(c.vibratoWaveform, c.vibratoPhase) * c.vibratoDepth) >> 7
		c.vibratoPhase = (c.vibratoPhase + c.vibratoSpeed) & 63
	case effectPortaToNoteVolSlide:
		c.portaToNote()
		c.volumeSlide()
	case effectTremolo:
		c.tremoloAdjust = (sineTable[c.tremoloPhase&31] * c.tremoloDepth) >> 6
		if c.tremoloPhase > 32 {
			c.tremoloAdjust = -c.tremoloAdjust
		}
		c.tremoloPhase = (c.tremoloPhase + c.tremoloSpeed) & 63
	case effectVolumeSlide:
		c.volumeSlide()
	case effectS3MVolumeSlide:
		// Fine slides are not applied on in between ticks
		x := c.memVolSlide >> 4
		y := c.memVolSlide & 0xF
		if x == 0xF || y == 0xF {
			break
		}

		// Dxy
		if x > 0 && y == 0 {
			// slide the volume up by x units
			c.volume += int(x)
			if c.volume > maxVolume {
				c.volume = maxVolume
			}
		}
		if x == 0 && y > 0 {
			// slide the volume down by y units
			c.volume -= int(y)
			if c.volume < 0 {
				c.volume = 0
			}
		}
	case effectS3MPortamentoDown:
		// Dxy
		// Fine and extra fine slides are not applied on in between ticks
		if c.memPortamento >= 0xE0 {
			break
		}
		c.period += int(c.memPortamento) * 4
		if c.period > 65535 {
			c.period = 65535
		}
	case effectS3MPortamentoUp:
		// Dxy
		// Fine and extra fine slides are not applied on in between ticks
		if c.memPortamento >= 0xE0 {
			break
		}
		c.period -= int(c.memPortamento) * 4
		if c.period < 1 {
			c.period = 1
		}
	case effectNoteRetrigVolSlide:
		if c.param > 0 {
			c.memRetrig = c.param
		}
		if c.effectCounter >= int(c.memRetrig&0xF) {
			c.triggerNote(c.period, c.sample, p.order, p.row, p.tick)
			c.volume = retrigVolume(int(c.memRetrig>>4), c.volume)
			c.effectCounter = 0
		}
	case effectExtended:
		switch c.param >> 4 {
		case effectExtendedNoteCut:
			if c.effectCounter == int(c.param&0xF) {
				c.volume = 0
			}
		case effectExtendedNoteDelay:
			if c.effectCounter == int(c.param&0xF) {
				c.triggerNote(c.periodToPlay, c.sampleToPlay, p.order, p.row, p.tick)
				c.volume = c.volumeToPlay
			}
		}
	}
}

// Returns if the end of the song was reached
func (p *Player) sequenceTick() bool {
	finished := false

	p.tick++
	if p.tick >= p.Speed {
		p.tick = 0

		p.row++
		if p.row >= 64 {
			p.row = 0
			p.order++
			p.ordersplayed++

			endOfSong := p.order >= len(p.Song.Orders)
			playLimitReached := p.PlayOrderLimit != -1 && p.ordersplayed >= p.PlayOrderLimit
			if endOfSong || playLimitReached {
				// End of the song reached, reset player state and stop
				finished = true
				p.reset()
			}
		}

		pattern := int(p.Song.Orders[p.order])
		rowDataIdx := p.rowDataIndex()

		loopChannel := -1 // Which channel index has an active loop, -1=no channel

		for i := 0; i < p.Song.Channels; i++ {
			channel := &p.channels[i]

			channel.effectCounter = 0

			patnote := &p.Song.patterns[pattern][rowDataIdx]
			sampNum := patnote.Sample
			pitch := patnote.Pitch
			effect := patnote.Effect
			param := patnote.Param

			notePresent := pitch > 0

			// Note triggering behavior, from experimentation in ST3
			//
			// Note Ins Vol Effect Behavior
			// N                   Play new note N with existing instrument, at
			//                     existing channel volume. If no prior
			//                     instrument nothing is played. Prior
			//                     instrument cleared at beginning of song, not
			//                     each pattern transition.
			//      I              Next note will use instrument I, stops
			//                     currently playing instrument if different.
			//                     If the same then the instrument continues
			//                     playing with the volume at the default.
			// N    I              Play new note N with new instrument I, at
			//                     instrument default volume.
			//          V          Adjust channel volume to V.
			// N        V          Play new note N at volume V with existing
			//                     instrument on channel.
			//      I   V          Next note will use instrument I, with volume
			//                     V (if no volume on the next note). Any
			//                     currently playing instrument is stopped.
			// N    I   V          Play new note N with new instrument I at new
			//                     volume V.
			// N            Dly    Play note N with delay using current
			//                     instrument and volume, currently playing
			//                     note is not changed.
			// N    I       Dly    Play note N with delay using instrument I at
			//                     default volume.
			//          V   Dly    After delay change volume of currently
			//                     playing note

			volume := noNoteVolume

			// If there is an instrument/sample number then reset the volume
			// sample numbers are 1-based in MOD & S3M formats
			if sampNum > 0 && sampNum <= len(p.Song.Samples) {
				smp := &p.Song.Samples[sampNum-1]

				channel.sampleToPlay = sampNum - 1
				volume = smp.Volume // Play at the instrument's volume

				// If there is no note and the instrument isn't the same as the active
				// instrument then stop the playing the current note.
				if !notePresent && channel.sample != (sampNum-1) {
					channel.sample = -1
				}
			}

			// If the note has a volume then use that
			if patnote.Volume != noNoteVolume {
				volume = patnote.Volume
			}

			noteDelay := effect == effectExtended && param>>4 == effectExtendedNoteDelay
			noteRetrigMem := effect == effectNoteRetrigVolSlide && param == 0
			portaToNote := effect == effectPortaToNote
			portaToNoteVolSlide := effect == effectPortaToNote // TODO: typo?
			playImmediately := !portaToNote && !portaToNoteVolSlide && !noteDelay

			channel.periodToPlay = channel.period

			// If there is a note pitch...
			if notePresent {
				// Convert the pitch to a period
				var period int
				if channel.sampleToPlay >= 0 {
					period = periodFromPlayerNote(pitch, p.Song.Samples[channel.sampleToPlay].C4Speed)
				}

				// ... save it away as the porta to note destination
				channel.portaPeriod = period

				// ... restart the sample if effect isn't 3, 5 or 0xEDx
				if playImmediately {
					switch pitch {
					case noteKeyOff:
						volume = 0 // set volume to 0
					case 255:
						// We should never get this because S3M loader remapped to 0
					}

					// ... assign the new instrument if one was provided
					channel.triggerNote(period, channel.sampleToPlay, p.order, p.row, p.tick)
				} else {
					channel.periodToPlay = period
				}
			} else {
				if noteRetrigMem {
					channel.triggerNote(channel.period, channel.sample, p.order, p.row, p.tick)
					channel.volume = retrigVolume(int(channel.memRetrig>>4), channel.volume)
				}
			}

			// This goes here because sometimes we don't have a note
			if volume != noNoteVolume {
				if noteDelay {
					channel.volumeToPlay = volume
				} else {
					channel.volume = volume
					channel.volumeToPlay = channel.volume
				}
			}

			channel.effect = effect
			channel.param = param

			// Reset on the new row
			channel.vibratoAdjust = 0
			channel.tremoloAdjust = 0

			switch effect {
			case effectPortaToNote:
				if param > 0 {
					channel.portaSpeed = int(param)
				}
			case effectVibrato:
				if param&0xF0 > 0 {
					channel.vibratoSpeed = int(param >> 4)
				}
				if param&0xF > 0 {
					channel.vibratoDepth = int(param & 0xF)
				}
			case effectTremolo:
				if param&0xF0 > 0 {
					channel.tremoloSpeed = int(param >> 4)
				}
				if param&0xF > 0 {
					channel.tremoloDepth = int(param & 0xF)
				}
			case effectSetSpeed:
				if param >= 0x20 {
					p.setTempo(int(param))
				} else {
					p.Speed = int(param)
					// TODO - what to do with p.tick here?
					//p.tick = p.Speed
				}
			case effectSetPanPosition:
				// TODO - support surround which is 0xA4?
				if param > 0x80 {
					param = 0x80
				}
				channel.pan = int(param)
			case effectSampleOffset:
				// TODO: clamp samplePosition to end of sample
				channel.samplePosition = uint(param) << 24
			// case effectSetVolume:
			// 	channel.volume = int(param)
			case effectJumpToPattern:
				// TODO - this effect currently activates on tick 0 and before
				// the rest of the channels have been processed. Experimentation
				// shows that this should activate on the last intermediate tick
				// and probably after all channels have been processed.
				p.order = int(param)
				if p.order >= len(p.Orders) {
					p.order = len(p.Orders) - 1
				}
				// TODO - what to do with p.tick here?
				p.row = 0
			case effectPatternBrk:
				// TODO - this effect currently activates on tick 0 and before
				// the rest of the channels have been processed. Experimentation
				// shows that this should activate on the last intermediate tick
				// and probably after all channels have been processed.

				// TODO - handle edge cases of multiple pattern break effects in
				// a row (only advance one time) and case of both Jump to
				// Pattern and Pattern Break in same row

				// Advance to the next pattern in the order unless we are on the
				// last pattern, in which case we stay on this pattern. This
				// behavior matches MilkyTracker.
				p.order++
				if p.order == len(p.Orders) {
					p.order = len(p.Orders) - 1
				}

				// This code can race, we subtract 1 to offset the row counter
				// increment after effect processing. If the player position is
				// read (e.g. generating audio) after processing this effect and
				// incrementing the row counter below then an invalid row will
				// be used. Other code that uses the row clamps to 0 but it
				// would be ideal to find a way to eliminate the race.
				p.row = int((param>>4)*10+param&0xF) - 1
				if p.row >= 64 {
					p.row = -1
				}
				// TODO - what to do with p.tick here?
			case effectPatternLoop:
				if param == 0 {
					p.loop[i].start = p.row
				} else {
					if p.loop[i].count > 0 {
						// There is already a count set
						p.loop[i].count = p.loop[i].count - 1
						if p.loop[i].count > 0 {
							// Have a row to jump to
							loopChannel = i
						}
					} else {
						p.loop[i].count = int(param)
						loopChannel = i
					}
				}
			case effectExtended:
				switch param >> 4 {
				case effectExtendedVibratoWaveform:
					if param&0xF < 4 {
						channel.vibratoWaveform = vibType(param & 0xF)
					}
					// TODO - retrig controls
					break
				case effectExtendedFineVolSlideUp:
					vol := channel.volume
					vol += int(param & 0x0F)
					if vol > maxVolume {
						vol = maxVolume
					}
					channel.volume = vol
				case effectExtendedFineVolSlideDown:
					vol := channel.volume
					vol -= int(param & 0xF)
					if vol < 0 {
						vol = 0
					}
					channel.volume = vol
				case effectExtendedNoteCut:
					if param&0xF == 0 {
						channel.volume = 0
					}
				}
			case effectS3MVolumeSlide:
				if param > 0 {
					channel.memVolSlide = param
				}

				// On first tick we only apply the fine volume slide
				x := channel.memVolSlide >> 4
				y := channel.memVolSlide & 0xF
				if x != 0xF && y != 0xF {
					break
				}

				// Dxy
				// DF1 slide down by 1 unit on tick 0
				// DFF is a special case and means slide up by F units on tick 0
				if x == 0xF && y != 0xF {
					// slide volume down by y units
					channel.volume -= int(y)
					if channel.volume < 0 {
						channel.volume = 0
					}
				}
				// D2F slide up by 2 units on tick 0
				if y == 0xF {
					// slide volume up by x units
					channel.volume += int(x)
					if channel.volume > maxVolume {
						channel.volume = maxVolume
					}
				}
			case effectS3MPortamentoDown:
				if param > 0 {
					channel.memPortamento = param
				}
				// Exy
				// EEy - on tick 0, extra fine slide down by y units
				// EFy - on tick 0, fine slide down by y*4 units
				if channel.memPortamento < 0xE0 {
					break
				}
				switch channel.memPortamento >> 4 {
				case 0xE: // extra fine slide
					channel.period += int(channel.memPortamento & 0xF)
				case 0xF: // fine slide
					channel.period += int(channel.memPortamento&0xF) * 4
				}
				if channel.period > 65535 {
					channel.period = 65535
				}
			case effectS3MPortamentoUp:
				if param > 0 {
					channel.memPortamento = param
				}
				// Fxy
				// FEy - on tick 0, extra fine slide down by y units
				// FFy - on tick 0, fine slide down by y*4 units
				if channel.memPortamento < 0xE0 {
					break
				}
				switch channel.memPortamento >> 4 {
				case 0xE: // extra fine slide
					channel.period -= int(channel.memPortamento & 0xF)
				case 0xF: // fine slide
					channel.period -= int(channel.memPortamento&0xF) * 4
				}
				if channel.period < 1 {
					channel.period = 1
				}
			case effectS3MGlobalVolume:
				p.globalVolume = uint(param)
				if p.globalVolume > maxVolume {
					p.globalVolume = maxVolume
				}
			}
			rowDataIdx++
		}

		if loopChannel >= 0 {
			p.row = p.loop[loopChannel].start - 1 // -1 for the ++ below
		}
	} else {
		// channel tick
		for i := 0; i < p.Song.Channels; i++ {
			p.channelTick(&p.channels[i], i, p.tick)
		}
	}

	return finished
}

func (c *channel) triggerNote(period, sample, order, row, tick int) {
	c.period = period
	c.sample = sample
	c.samplePosition = 0
	c.tremoloPhase = 0
	c.vibratoPhase = 0
	c.trigOrder = order
	c.trigRow = row
	c.trigTick = tick
}

func (p *Player) mixChannels(nSamples, offset int) {
	for ci := range p.channels {
		channel := &p.channels[ci]

		if channel.sample == -1 {
			continue
		}

		sample := &p.Song.Samples[channel.sample]
		if sample.Length == 0 {
			continue
		}

		period := channel.period + (channel.vibratoAdjust * 4)
		playbackHz := int(retracePALHz / float32(period))
		dr := uint(playbackHz<<16) / p.samplingFrequency
		pos := channel.samplePosition
		vol := channel.volume + channel.tremoloAdjust
		vol = (vol * p.GlobalVolume) >> 6
		if vol >= maxVolume {
			vol = maxVolume
		}

		// If the volume is off or the channel muted
		if vol <= 0 || (p.Mute&(1<<ci)) != 0 {
			channel.samplePosition = pos + dr*uint(nSamples)
			continue
		}
		vol *= int(p.volBoost)

		lvol := ((127 - channel.pan) * vol) >> 7
		rvol := (channel.pan * vol) >> 7
		if lvol == 0 && rvol == 0 {
			// lvol and rvol can end up 0 for very quiet volumes due to
			// precision issues, so skip the mix loop.
			// TODO: Eliminate the two separate volume checks
			channel.samplePosition = pos + dr*uint(nSamples)
			continue
		}

		var sampEnd uint
		if sample.LoopLen > 0 {
			sampEnd = uint(sample.LoopStart+sample.LoopLen) << 16
		} else {
			sampEnd = uint(sample.Length) << 16
		}

		cur := offset * 2
		end := (offset + nSamples) * 2

		for cur < end {
			// Compute the position in the sample by end
			epos := pos + uint((end-cur)/2)*dr
			// If the sample ends before the end of this loop iteration only run to that
			if epos >= sampEnd {
				epos = sampEnd
			}

			// lvol rvol | case
			//   0    0  |  skip, nothing to mix in. already handled above
			//  127   0  |  mono mix left side
			//   0   127 |  mono mix right side
			//   N    N  |  stereo mix
			if lvol != 0 && rvol == 0 || lvol == 0 && rvol != 0 {
				if lvol != 0 {
					vol = lvol
				} else {
					vol = rvol
					cur++
				}
				for pos < epos {
					sd := int(sample.Data[pos>>16])
					p.mixbuffer[cur] += sd * vol

					pos += dr
					cur += 2
				}
				// Now snap cursor to the correct position
				if rvol != 0 {
					cur--
				}
			} else {
				for pos < epos {
					// WARNING: no clamping when mixing into mixbuffer. Clamping will be applied when the final audio is returned
					// to the caller.
					sd := int(sample.Data[pos>>16])
					p.mixbuffer[cur+0] += sd * lvol
					p.mixbuffer[cur+1] += sd * rvol

					pos += dr
					cur += 2
				}
			}
			if pos >= sampEnd {
				if sample.LoopLen > 0 {
					pos = uint(sample.LoopStart) << 16
				} else {
					channel.sample = -1 // turn off the channel
					break
				}
			}
		}
		channel.samplePosition = pos
	}
}

// GenerateAudio fills out with stereo sample data (LRLRLR...) and returns the
// number of stereo samples generated.
//
// This function also advances the player through the song. If the player is
// paused it will generate 0 samples. In the case that the player reaches the
// end of the song it may generate less samples than the buffer can hold.
func (p *Player) GenerateAudio(out []int16) int {
	if !p.playing {
		return 0
	}

	if len(out) > len(p.mixbuffer) {
		// TODO - better handling of this error condition, e.g. resizing the mix buffer
		panic(fmt.Sprintf("Mixbuffer too small %d wanted %d size", len(out), len(p.mixbuffer)))
	}

	// Zero out the portion of the mixbuffer that will be written to.
	clear(p.mixbuffer[0:len(out)])

	count := len(out) / 2 // L&R samples are interleaved, so out length 2 is asking for one stereo sample
	offset := 0
	generated := 0

	for count > 0 {
		if p.tickSamplePos == p.samplesPerTick {
			if p.sequenceTick() {
				break // song finished, exit
			}
			p.tickSamplePos = 0
		}

		remain := p.samplesPerTick - p.tickSamplePos
		if remain > count {
			remain = count
		}
		p.mixChannels(remain, offset)

		p.tickSamplePos += remain
		offset += remain
		generated += remain
		count -= remain
	}

	// Downsample the mix buffer into the output buffer
	p.downsample(out, generated*2)

	return generated
}

func (p *Player) downsample(out []int16, generated int) {
	for i, s := range p.mixbuffer[0:generated] {
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		out[i] = int16(s)
	}
}

// There is a race condition where the row counter can be set to -1 and then
// used resulting in invalid offsets. This function protects against that
// issue but it would be ideal to eliminate the race condition.
func (p *Player) rowDataIndex() int {
	rc := p.row
	if rc < 0 {
		rc = 0
	}

	return rc * p.Song.Channels
}

// Allocate and initialize a new pattern of notes
func initNotePattern(n int) []note {
	notes := make([]note, n)
	for i := range notes {
		notes[i].Volume = noNoteVolume
	}

	return notes
}

//lint:ignore U1000 Keeping this around for research
func periodFromS3MNoteOld(note byte) int {
	s3mnote := note & 0xF
	s3moctave := note >> 4
	s3mperiod := 8363 * 4 * (s3mPeriodTable[s3mnote] >> s3moctave) / 8383 // TODO: support finetune
	return s3mperiod
}

// Converts an player internal note representation into an Amiga MOD period.
// This code is inspired by libxmp.
func periodFromPlayerNote(note playerNote, c4speed int) int {
	// This formula is the inverse of the formula in periodToPlayerNote().
	period := periodBase / math.Pow(2, float64(note)/12.0)
	period = (8363 * period) / float64(c4speed) // Perform finetuning
	return int(period) * 4
}

// pos runs from 0 to 63
func vibratoFn(waveform vibType, pos int) (vib int) {
	switch waveform {
	case vibratoSine:
		vib = sineTable[pos&31]
		if pos > 32 {
			vib = -vib
		}
	case vibratoRampDown:
		// Determined by listening tests in ST3
		vib = -((63 - (pos * 2)) * 4)
	case vibratoSquareWave:
		// Determined by listening tests in ST3
		vib = 255
		if pos > 32 {
			vib = 0
		}
	default:
		// Random not supported
	}

	return
}

func retrigVolume(mode, vol int) (outvol int) {
	switch mode {
	case 1:
		outvol = vol - 1
	case 2:
		outvol = vol - 2
	case 3:
		outvol = vol - 4
	case 4:
		outvol = vol - 8
	case 5:
		outvol = vol - 16
	case 6:
		outvol = (vol * 2) / 3
	case 7:
		outvol = vol / 2
	case 9:
		outvol = vol + 1
	case 10:
		outvol = vol + 2
	case 11:
		outvol = vol + 4
	case 12:
		outvol = vol + 8
	case 13:
		outvol = vol + 16
	case 14:
		outvol = (vol * 3) / 2
	case 15:
		outvol = vol * 2
	default:
		outvol = vol
	}
	if outvol < 0 {
		outvol = 0
	}
	if outvol > maxVolume {
		outvol = maxVolume
	}

	return
}

func dumpf(format string, a ...interface{}) {
	if dumpW == nil {
		return
	}

	fmt.Fprintf(dumpW, format, a...)
}

func noteStrFromPeriod(period int) string {
	for i, prd := range periodTable {
		if prd == period {
			return fmt.Sprintf("%s%d", notes[i%12], i/12+2)
		}
	}

	return "   "
}

// Useful function to dump contents of the audio buffer
// tcur = the absolute offset (in samples) in the song of the output buffer
// ns = number of samples to print
//
//lint:ignore U1000 Keep around for debugging
func dumpChannel(tcur, ns int, out []int16) {
	fmt.Printf("%d: ", tcur)
	for i := 0; i < ns; i++ {
		a := uint16(out[i*2+0])
		a = (a&0xFF)<<8 | (a >> 8)
		b := uint16(out[i*2+1])
		b = (b&0xFF)<<8 | (b >> 8)
		fmt.Printf("%04X%04X", a, b)
		if i == ns-1 || ((i > 0) && (i%8) == 7) {
			fmt.Println()
			if i != ns-1 {
				fmt.Printf("%d: ", tcur+i+1)
			}
		}
	}
}
