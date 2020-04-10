package mod

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	retraceNTSCHz = 7159090.5 // Amiga NTSC vertical retrace timing
	globalVolume  = 16        // Hack for now to boost volume

	// MOD note effects
	effectPortamentoUp        = 0x1
	effectPortamentoDown      = 0x2
	effectPortaToNote         = 0x3
	effectPortaToNoteVolSlide = 0x5
	effectSampleOffset        = 0x9
	effectVolumeSlide         = 0xA
	effectSetVolume           = 0xC
	effectPatternBrk          = 0xD
	effectExtended            = 0xE
	effectSetSpeed            = 0xF

	effectExtendedFineVolSlideUp   = 0xA
	effectExtendedFineVolSlideDown = 0xB
	effectExtendedNoteCut          = 0xC
)

type Player struct {
	*Song
	samplingFrequency uint

	// song configuration
	Tempo          int
	Speed          int
	samplesPerTick int

	// These next fields track player position in the song
	tickSamplePos int // the number of samples in the tick
	tick          int // decrementing counter for number of ticks per row
	rowCounter    int // which row in the order
	ordIdx        int // current order of the song

	Mute uint // bitmask of muted channels, channel 1 in LSB

	EndCh chan struct{} // indicates end of song reached

	channels []channel
}

type channel struct {
	sampleIdx      int
	period         int
	portaPeriod    int // Portamento destination as a period
	portaSpeed     int
	volume         int
	pan            int // Pan position, 0=Full Left, 127=Full Right
	fineTune       int
	samplePosition uint

	effect        byte
	param         byte
	effectCounter int
}

// A song currently represents a MOD file, will need revising if S3M support is added
type Song struct {
	Title     string
	nChannels int
	nOrders   int
	orders    [128]byte
	nPatterns int
	Tempo     int // in beats per minute
	Speed     int // number of tempo ticks before advancing to the next row

	samples  [31]Sample
	patterns []byte
}

type Sample struct {
	name      string
	length    int
	fineTune  int
	volume    int
	loopStart int
	loopLen   int
	data      []int8
}

var (
	ErrUnrecognizedMODFormat = errors.New("Unrecognized MOD format")

	// Amiga period values. This table is used to map the note period
	// in the MOD file to a note index
	periodTable = []int{
		// C-1, C#1, D-1, ..., B-1
		856, 808, 762, 720, 678, 640, 604, 570, 538, 508, 480, 453,
		// C-2, C#2, D-2, ..., B-2
		428, 404, 381, 360, 339, 320, 302, 285, 269, 254, 240, 226,
		// C-3, C#3, D-3, ..., B-3
		214, 202, 190, 180, 170, 160, 151, 143, 135, 127, 120, 113,
	}

	// Fine tuning values from Micromod. Fine tuning goes from -8
	// to +7 with 0 (no fine tuning) in the middle at index 8. The
	// values are .12 fixed point and used to scale the note period.
	// A fine tuning value of -8 is equal to the next lower note.
	fineTuning = []int{
		4340, 4308, 4277, 4247, 4216, 4186, 4156, 4126,
		4096, 4067, 4037, 4008, 3979, 3951, 3922, 3894,
	}

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

func NewPlayer(song *Song, samplingFrequency uint) *Player {
	player := &Player{samplingFrequency: samplingFrequency, Song: song, Speed: 6}
	player.setTempo(125)
	player.channels = make([]channel, song.nChannels)
	for i := 0; i < song.nChannels; i++ {
		channel := &player.channels[i]
		channel.sampleIdx = -1
		switch i & 3 {
		case 0, 3:
			channel.pan = 0
		case 1, 2:
			channel.pan = 127
		}
	}

	player.EndCh = make(chan struct{}, 1)

	return player
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
	case effectPortaToNoteVolSlide:
		c.portaToNote()
		c.volumeSlide()
	case effectVolumeSlide:
		c.volumeSlide()

	case effectExtended:
		switch c.param >> 4 {
		case effectExtendedNoteCut:
			if c.effectCounter == int(c.param&0xF) {
				c.volume = 0
			}
		}
	}
}

func (p *Player) sequenceTick() {
	p.tick--
	if p.tick <= 0 {
		p.tick = p.Speed

		pattern := int(p.Song.orders[p.ordIdx])
		rowDataIdx := (pattern*64 + p.rowCounter) * 4 * p.Song.nChannels

		fmt.Printf("%02X %02X|", p.ordIdx, p.rowCounter)

		for i := 0; i < p.Song.nChannels; i++ {
			channel := &p.channels[i]

			channel.effectCounter = 0
			sampNum, period, effect, param := decodeNote(p.Song.patterns[rowDataIdx : rowDataIdx+4])

			// Getting note triggering logic correct was a pain, H/T micromod

			// If there is an instrument/sample number then reset the volume
			// sample numbers are 1-based in MOD format
			if sampNum > 0 && sampNum < 32 {
				smp := &p.Song.samples[sampNum-1]

				channel.volume = smp.volume
				channel.fineTune = smp.fineTune
			}

			// If there is a period...
			if period > 0 {
				// ... save it away as the porta to note destination
				channel.portaPeriod = period
				// ... restart the sample if effect isn't 3, 5 or 0xEDx
				if effect != effectPortaToNote && effect != effectPortaToNoteVolSlide && !(effect == 0xE && param>>4 == 0xD) {
					channel.samplePosition = 0

					// ... reset the period
					channel.period = (period * fineTuning[channel.fineTune]) >> 12

					// ... assign the new instrument if one was provided
					if sampNum > 0 && sampNum < 32 {
						channel.sampleIdx = sampNum - 1
					}
				}
			}
			channel.effect = effect
			channel.param = param

			if i < 4 {
				fmt.Printf("%s %2X %X%02X", noteStr(periodToNote(period)), sampNum, effect, param)
				if i < 3 {
					fmt.Print("|")
				}
			} else {
				if i == 4 {
					fmt.Print(" ...")
				}
			}

			switch effect {
			case effectPortaToNote:
				if param > 0 {
					channel.portaSpeed = int(param)
				}
			case effectSetSpeed:
				if param >= 0x20 {
					p.setTempo(int(param))
				} else {
					p.Speed = int(param)
				}
			case effectSampleOffset:
				channel.samplePosition = uint(param) << 24
			case effectSetVolume:
				channel.volume = int(param)
			case effectPatternBrk:
				p.ordIdx++
				// TODO handle looping
				p.rowCounter = int((param>>4)*10 + param&0xF)
				// TODO skipping first row of pattern?
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
			rowDataIdx += 4
		}
		fmt.Println()

		p.rowCounter++
		if p.rowCounter >= 64 {
			p.rowCounter = 0
			p.ordIdx++
			if p.ordIdx >= p.Song.nOrders {
				p.EndCh <- struct{}{}
				// TODO: loop and reset instruments to default state
			}
		}
	} else {
		// channel tick
		for i := 0; i < p.Song.nChannels; i++ {
			p.channelTick(&p.channels[i], i)
		}
	}
}

func (p *Player) mixChannels(out []int16, nSamples, offset int) {
	for s := offset * 2; s < (offset+nSamples)*2; s += 2 {
		out[s+0] = 0
		out[s+1] = 0
	}

	for chanIdx := range p.channels {
		channel := &p.channels[chanIdx]

		if channel.sampleIdx == -1 || channel.volume == 0 {
			continue
		}

		if (p.Mute & (1 << chanIdx)) != 0 {
			continue
		}

		sample := &p.Song.samples[channel.sampleIdx]
		if sample.length == 0 {
			continue
		}

		playbackHz := int(retraceNTSCHz / float32(channel.period*2))
		dr := uint(playbackHz<<16) / p.samplingFrequency
		pos := channel.samplePosition
		lvol := ((127 - channel.pan) * channel.volume) >> 7
		rvol := (channel.pan * channel.volume) >> 7

		// TODO: Full pan left or right optimization in mixer
		// TODO: Move sample loop check outside of mixer inner loop
		for off := offset * 2; off < (offset+nSamples)*2; off += 2 {
			// WARNING: no clipping protection when mixing in the sample (hence the downshift)
			samp := int(sample.data[pos>>16])
			out[off+0] += int16((samp * lvol) >> 2)
			out[off+1] += int16((samp * rvol) >> 2)

			pos += dr
			if pos >= uint(sample.length<<16) {
				if sample.loopLen > 0 {
					pos = uint(sample.loopStart) << 16
				} else {
					channel.sampleIdx = -1 // turn off the channel
					break
				}
			}
		}
		channel.samplePosition = pos
	}
}

func (p *Player) GenerateAudio(out []int16) {
	count := len(out) / 2 // portaudio counts L & R channels separately, length 2 means one stereo sample
	offset := 0
	for count > 0 {
		remain := p.samplesPerTick - p.tickSamplePos
		if remain > count {
			remain = count
		}

		p.mixChannels(out, remain, offset)
		offset += remain

		p.tickSamplePos += remain
		if p.tickSamplePos == p.samplesPerTick {
			p.sequenceTick()
			p.tickSamplePos = 0
		}
		count -= remain
	}
}

func decodeNote(note []byte) (int, int, byte, byte) {
	sampNum := note[0]&0xF0 + note[2]>>4
	period := int(int(note[0]&0xF)<<8 + int(note[1]))
	effNum := note[2] & 0xF
	effParm := note[3]

	return int(sampNum), period, effNum, effParm
}

// Turn a note index into a string representation, e.g. 'C-4' or 'F#3'
// Returns a blank string of three spaces if the note index is -1
func noteStr(note int) string {
	if note == -1 {
		return "   "
	}

	return fmt.Sprintf("%s%d", notes[note%12], note/12+3)
}

// Convert amiga period to note index
func periodToNote(period int) int {
	for i, prd := range periodTable {
		if prd == period {
			return i
		}
	}

	return -1
}

func readSampleInfo(r *bytes.Reader) (*Sample, error) {
	var smp Sample
	tmp := make([]byte, 22)
	var err error
	if _, err = r.Read(tmp); err != nil {
		return nil, err
	}
	smp.name = string(tmp)

	if _, err = r.Read(tmp[:2]); err != nil {
		return nil, err
	}
	smp.length = int(binary.BigEndian.Uint16(tmp)) * 2

	var b byte
	if b, err = r.ReadByte(); err != nil {
		return nil, err
	}
	smp.fineTune = int(b&7) - int(b&8) + 8

	if b, err = r.ReadByte(); err != nil {
		return nil, err
	}
	smp.volume = int(b)

	if _, err = r.Read(tmp[:2]); err != nil {
		return nil, err
	}
	smp.loopStart = int(binary.BigEndian.Uint16(tmp)) * 2

	if _, err = r.Read(tmp[:2]); err != nil {
		return nil, err
	}
	smp.loopLen = int(binary.BigEndian.Uint16(tmp)) * 2

	// Sanitize sample loop info (H/T micromod)
	if smp.loopLen < 4 {
		smp.loopLen = 0
	}

	return &smp, nil
}

func NewSongFromBytes(songBytes []byte) (*Song, error) {
	hdr := &Song{Speed: 6, Tempo: 125}

	buf := bytes.NewReader(songBytes)
	y := make([]byte, 20)
	buf.Read(y)
	hdr.Title = string(y)

	// Read sample information (sample data is read later)
	for i := 0; i < 31; i++ {
		s, err := readSampleInfo(buf)
		if err != nil {
			return nil, err
		}
		hdr.samples[i] = *s
	}

	// Read orders
	no, err := buf.ReadByte()
	if err != nil {
		return nil, err
	}

	hdr.nOrders = int(no)
	buf.ReadByte() // Discard
	if n, err := buf.Read(hdr.orders[:]); n != len(hdr.orders) || err != nil {
		return nil, err
	}

	hdr.nPatterns = int(hdr.orders[0])
	for i := 1; i < 128; i++ {
		if int(hdr.orders[i]) > hdr.nPatterns {
			hdr.nPatterns = int(hdr.orders[i])
		}
	}

	// Detect number of channels
	x := make([]byte, 4)
	if n, err := buf.Read(x); n != 4 || err != nil {
		return nil, err
	}
	switch string(x[2:]) {
	case "K.": // M.K.
		hdr.nChannels = 4
	case "HN": // xCHN, x = number of channels
		hdr.nChannels = (int(x[0]) - 48)
	case "CH": // xxCH, xx = number of channels as two digit decimal
		hdr.nChannels = (int(x[0])-48)*10 + (int(x[1] - 48))
	default:
		return nil, ErrUnrecognizedMODFormat
	}

	// Read pattern data
	hdr.patterns = make([]byte, hdr.nChannels*(hdr.nPatterns+1)*64*4)
	buf.Read(hdr.patterns)

	// Read sample data
	for i := 0; i < 31; i++ {
		tmp := make([]byte, hdr.samples[i].length)
		buf.Read(tmp)

		hdr.samples[i].data = make([]int8, hdr.samples[i].length)
		for j, sd := range tmp {
			hdr.samples[i].data[j] = int8(sd)
		}
	}

	return hdr, nil
}
