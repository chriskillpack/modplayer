package modplayer

// Something else to look at https://www.celersms.com/doc/XM_file_format.pdf

import (
	"fmt"
	"math"
)

const (
	retracePALHz = 7093789.2 // Amiga PAL vertical retrace timing

	rowsPerPattern = 64

	// MOD note effects
	effectPortamentoUp        = 0x1
	effectPortamentoDown      = 0x2
	effectPortaToNote         = 0x3
	effectVibrato             = 0x4 // TODO: Complete
	effectPortaToNoteVolSlide = 0x5
	effectTremolo             = 0x7 // TODO: Complete
	effectSampleOffset        = 0x9
	effectVolumeSlide         = 0xA
	effectJumpToPattern       = 0xB
	effectSetVolume           = 0xC
	effectPatternBrk          = 0xD
	effectExtended            = 0xE
	effectSetSpeed            = 0xF

	// Extended effects (Exy), x = effect, y effect param
	effectExtendedNoteRetrig       = 0x9
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
	playing       bool

	// Bitmask of muted channels, channel 1 in LSB. To mute a channel set
	// its bit to 1.
	Mute uint

	channels []channel
}

// ChannelNoteData represents the note data for a channel
type ChannelNoteData struct {
	Note       string // 'A-4', 'C#3', ...
	Instrument int    // -1 if no instrument
	Effect     int
	Param      int
}

// String returns a formatted string of the note data
func (c *ChannelNoteData) String() string {
	return fmt.Sprintf("%s %2X %X%02X", c.Note, c.Instrument, c.Effect, c.Param)
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

// Internal representation of a pattern note
type note struct {
	Sample int
	Period int
	Volume int // Unused by MOD files, FF=no value set, ignore
	Effect byte
	Param  byte
}

type channel struct {
	sample         int // sample that is being played (or -1 if no sample)
	sampleToPlay   int // sample _to be played_, used for Note Delay effect
	period         int
	portaPeriod    int // Portamento destination as a period
	portaSpeed     int
	volume         int
	pan            int // Pan position, 0=Full Left, 127=Full Right
	fineTune       int
	samplePosition uint

	tremoloDepth  int
	tremoloSpeed  int
	tremoloPhase  int
	tremoloAdjust int

	vibratoDepth  int
	vibratoSpeed  int
	vibratoPhase  int
	vibratoAdjust int

	effect        byte
	param         byte
	effectCounter int

	trigOrder, trigRow int // The order and row the channel was triggered on
	// This is here only for State().
}

// Song represents a MOD file
// Will need revising if S3M support is added
type Song struct {
	Title    string
	Channels int
	Orders   []byte
	Tempo    int // in beats per minute
	Speed    int // number of tempo ticks before advancing to the next row

	Samples  []Sample
	patterns [][]note
}

// Sample holds information about an instrument sample including sample data
type Sample struct {
	Name      string
	Length    int
	FineTune  int
	Volume    int
	LoopStart int
	LoopLen   int
	C4Speed   int
	Data      []int8
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

	// Fine tuning values from Micromod. Fine tuning goes from -8
	// to +7 with 0 (no fine tuning) in the middle at index 8. The
	// values are .12 fixed point and used to scale the note period.
	// A fine tuning value of -8 is equal to the next lower note.
	//lint:ignore U1000 This will be reused later
	fineTuning = []int{
		4340, 4308, 4277, 4247, 4216, 4186, 4156, 4126,
		4096, 4067, 4037, 4008, 3979, 3951, 3922, 3894,
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
)

func (c *channel) portaToNote() {
	period := c.period
	if period < c.portaPeriod {
		period += c.portaSpeed
		if period > c.portaPeriod {
			period = c.portaPeriod
		}
	} else if period > c.portaPeriod {
		period -= c.portaSpeed
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
		if vol > 64 {
			vol = 64
		}
	} else if c.param != 0 {
		vol -= int(c.param & 0xF)
		if vol < 0 {
			vol = 0
		}
	}
	c.volume = vol
}

// NewPlayer returns a new Player for the given song. The Player is already
// started.
func NewPlayer(song *Song, samplingFrequency uint) (*Player, error) {
	player := &Player{
		samplingFrequency: samplingFrequency,
		volBoost:          1,
		Song:              song,
		Speed:             6,
	}

	player.channels = make([]channel, song.Channels)

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
		note.Note = noteStr(patnote.Period)
		note.Instrument = patnote.Sample
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
	p.row = row
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
		note.Note = noteStr(patnote.Period)
		note.Instrument = patnote.Sample
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
	p.row = 0

	for i := 0; i < p.Song.Channels; i++ {
		channel := &p.channels[i]
		channel.sample = -1
		channel.tremoloDepth = 0
		channel.tremoloSpeed = 0
		channel.tremoloPhase = 0
		channel.tremoloAdjust = 0
		channel.vibratoDepth = 0
		channel.vibratoSpeed = 0
		channel.vibratoPhase = 0
		channel.vibratoAdjust = 0

		switch i & 3 {
		case 0, 3:
			channel.pan = 0 // left
		case 1, 2:
			channel.pan = 127 // right
		}
	}
}

func (p *Player) setTempo(tempo int) {
	p.samplesPerTick = int((p.samplingFrequency<<1)+(p.samplingFrequency>>1)) / tempo
	p.Tempo = tempo
}

func (p *Player) channelTick(c *channel, ci int) {
	c.effectCounter++

	switch c.effect {
	case effectPortamentoUp:
		c.period -= int(c.param)
		if c.period < 1 {
			c.period = 1
		}
	case effectPortamentoDown:
		c.period += int(c.param)
		if c.period > 65535 {
			c.period = 65535
		}
	case effectPortaToNote:
		c.portaToNote()
	case effectVibrato:
		c.vibratoAdjust = (sineTable[c.vibratoPhase&31] * c.vibratoDepth) >> 7
		if c.vibratoPhase > 32 {
			c.vibratoAdjust = -c.vibratoAdjust
		}
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

	case effectExtended:
		switch c.param >> 4 {
		case effectExtendedNoteRetrig:
			if c.effectCounter >= int(c.param&0xF) {
				c.effectCounter = 0
				c.samplePosition = 0
			}
		case effectExtendedNoteCut:
			if c.effectCounter == int(c.param&0xF) {
				c.volume = 0
			}
		case effectExtendedNoteDelay:
			if c.effectCounter == int(c.param&0xF) {
				c.sample = c.sampleToPlay
				c.samplePosition = 0
				c.tremoloPhase = 0
				c.vibratoPhase = 0
			}
		}
	}
}

// Returns if the end of the song was reached
func (p *Player) sequenceTick() bool {
	finished := false

	p.tick--
	if p.tick <= 0 {
		p.tick = p.Speed

		pattern := int(p.Song.Orders[p.order])
		rowDataIdx := p.rowDataIndex()

		for i := 0; i < p.Song.Channels; i++ {
			channel := &p.channels[i]

			if (p.Mute & (1 << i)) != 0 {
				rowDataIdx++
				continue
			}

			channel.effectCounter = 0
			patnote := &p.Song.patterns[pattern][rowDataIdx]
			sampNum := patnote.Sample
			period := patnote.Period
			effect := byte(patnote.Effect)
			param := byte(patnote.Param)

			// Getting note triggering logic correct was a pain, H/T micromod

			// If there is an instrument/sample number then reset the volume
			// sample numbers are 1-based in MOD format
			if sampNum > 0 && sampNum < 32 {
				smp := &p.Song.Samples[sampNum-1]

				channel.volume = smp.Volume
				channel.fineTune = smp.FineTune
				channel.sampleToPlay = sampNum - 1
				channel.samplePosition = 0
			}

			// If there is a period...
			if period > 0 {
				// ... save it away as the porta to note destination
				channel.portaPeriod = periodFromPlayerNote(period)

				// ... restart the sample if effect isn't 3, 5 or 0xEDx
				if effect != effectPortaToNote && effect != effectPortaToNoteVolSlide &&
					!(effect == 0xE && param>>4 == effectExtendedNoteDelay) {
					channel.samplePosition = 0

					// ... reset the period
					// channel.period = (period * fineTuning[channel.fineTune]) >> 12

					// convert the S3M note to a period
					if period < 254 {
						channel.period = periodFromPlayerNote(period)
					}

					// ... assign the new instrument if one was provided
					channel.sample = channel.sampleToPlay
					channel.tremoloPhase = 0
					channel.vibratoPhase = 0
					channel.trigOrder = p.order
					channel.trigRow = p.row
				}
			}
			channel.effect = effect
			channel.param = param

			channel.vibratoAdjust = 0
			channel.tremoloAdjust = 0

			if patnote.Volume != 0xFF {
				channel.volume = patnote.Volume
			}

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
					p.tick = p.Speed
				}
			case effectSampleOffset:
				channel.samplePosition = uint(param) << 24
			// case effectSetVolume:
			// 	channel.volume = int(param)
			case effectJumpToPattern:
				p.order = int(param)
				if p.order >= len(p.Orders) {
					p.order = len(p.Orders) - 1
				}
				p.row = -1
			case effectPatternBrk:
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
			case effectExtended:
				switch param >> 4 {
				case effectExtendedFineVolSlideUp:
					vol := channel.volume
					vol += int(param & 0x0F)
					if vol > 64 {
						vol = 64
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
			}
			rowDataIdx++
		}

		p.row++
		if p.row >= 64 {
			p.row = 0
			p.order++

			if p.order >= len(p.Song.Orders) {
				// End of the song reached, reset player state and stop
				finished = true
				p.reset()
			}
		}
	} else {
		// channel tick
		for i := 0; i < p.Song.Channels; i++ {
			p.channelTick(&p.channels[i], i)
		}
	}

	return finished
}

func (p *Player) mixChannels(out []int16, nSamples, offset int) {
	// Zero out the portion of out that will be written to.
	// The compiler will identify the range loop form and replace with a
	// runtime.memclrNoHeapPointers call which has architecture specific
	// optimizations.
	o := out[offset*2 : (offset+nSamples)*2]
	for i := range o {
		o[i] = 0
	}

	for ci := range p.channels {
		channel := &p.channels[ci]

		if channel.sample == -1 {
			continue
		}

		sample := &p.Song.Samples[channel.sample]
		if sample.Length == 0 {
			continue
		}

		period := (channel.period + channel.vibratoAdjust) * 2
		playbackHz := int(retracePALHz / float32(period))
		dr := uint(playbackHz<<16) / p.samplingFrequency
		pos := channel.samplePosition
		vol := channel.volume + channel.tremoloAdjust
		if vol >= 64 {
			vol = 64
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

		sampEnd := uint(sample.Length << 16)

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
					out[cur] += int16(sd * vol)

					pos += dr
					cur += 2
				}
				// Now snap cursor to the correct position
				if rvol != 0 {
					cur--
				}
			} else {
				for pos < epos {
					// WARNING: no clamping when mixing, this seems to be the case in other players I looked at.
					// I think the expectation is that the musician not play samples too loudly.
					sd := int(sample.Data[pos>>16])
					out[cur+0] += int16(sd * lvol)
					out[cur+1] += int16(sd * rvol)

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

	count := len(out) / 2 // L&R samples are interleaved, so out length 2 is asking for one stereo sample
	offset := 0
	generated := 0

	for count > 0 {
		remain := p.samplesPerTick - p.tickSamplePos
		if remain > count {
			remain = count
		}

		p.mixChannels(out, remain, offset)
		offset += remain
		generated += remain

		p.tickSamplePos += remain
		if p.tickSamplePos == p.samplesPerTick {
			if p.sequenceTick() {
				count = remain // song finished, exit
			}
			p.tickSamplePos = 0
		}
		count -= remain
	}
	return generated
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
func initNotePattern(nch int) []note {
	notes := make([]note, rowsPerPattern*nch)
	for i := range notes {
		notes[i].Volume = 0xFF
	}

	return notes
}

// Compute the string representation of a note ('C-4', 'F#3', etc). Returns a
// string of three spaces if the note is unrecognized.
func noteStr(note int) string {
	if note == 0 {
		return "   "
	}

	return fmt.Sprintf("%s%d", notes[note%12], note/12-1)
}

//lint:ignore U1000 Keeping this around for research
func periodFromS3MNoteOld(note byte) int {
	s3mnote := note & 0xF
	s3moctave := note >> 4
	s3mperiod := 8363 * 4 * (s3mPeriodTable[s3mnote] >> s3moctave) / 8383 // TODO: support finetune
	return s3mperiod
}

// Converts an player internal note representation into an Amiga MOD periods
// This code is a complete lift from libxmp.
func periodFromPlayerNote(note int) int {
	return int(periodBase / math.Pow(2, float64(note)/12.0))
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
