// MOD player in Go
// Useful notes https://github.com/AntonioND/gbt-player/blob/master/mod2gbt/FMODDOC.TXT
// Uses portaudio for audio output

package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/chriskillpack/mod_player/wav"
	"github.com/gordonklaus/portaudio"
)

// ModHeader TODO
type ModHeader struct {
	title     string
	nChannels int
	nOrders   int
	orders    [128]byte
	nPatterns int
	tempo     int // in beats per minute
	speed     int // number of tempo ticks before advancing to the next row

	samples  [31]sample
	patterns []byte
}

type sample struct {
	name      string
	length    int
	fineTune  int
	volume    int
	loopStart int
	loopLen   int
	data      []int8
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

type Player struct {
	hdr               ModHeader
	samplingFrequency int

	// song configuration
	tempo          int
	speed          int
	samplesPerTick int

	// These next fields track player position in the song
	tickSamplePos int // the number of samples in the tick
	tick          int // decrementing counter for number of ticks per row
	rowCounter    int // which row in the order
	ordIdx        int // current order of the song

	mute uint // bitmask of muted channels, channel 1 in LSB

	endCh chan struct{} // indicates end of song reached

	channels []channel
}

func (p *Player) setTempo(tempo int) {
	p.samplesPerTick = ((p.samplingFrequency << 1) + (p.samplingFrequency >> 1)) / tempo
	p.tempo = tempo
}

func NewPlayer(song ModHeader, samplingFrequency int) *Player {
	player := &Player{samplingFrequency: samplingFrequency, hdr: song, speed: 6}
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

	player.endCh = make(chan struct{}, 1)

	return player
}

const (
	outputBufferHz      = 44100
	outputBufferSamples = 8192
	retraceNTSCHz       = 7159090.5 // Amiga NTSC vertical retrace timing
	globalVolume        = 16        // Hack for now to boost volume

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

var (
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

func decodeNote(note []byte) (int, int, byte, byte) {
	sampNum := note[0]&0xF0 + note[2]>>4
	period := int(int(note[0]&0xF)<<8 + int(note[1]))
	effNum := note[2] & 0xF
	effParm := note[3]

	return int(sampNum), period, effNum, effParm
}

func readSampleInfo(r *bytes.Reader) (*sample, error) {
	var smp sample
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

// Convert amiga period to note index
func periodToNote(period int) int {
	for i, prd := range periodTable {
		if prd == period {
			return i
		}
	}

	return -1
}

// Turn a note index into a string representation, e.g. 'C-4' or 'F#3'
// Returns a blank string of three spaces if the note index is -1
func noteStr(note int) string {
	if note == -1 {
		return "   "
	}

	return fmt.Sprintf("%s%d", notes[note%12], note/12+3)
}

// TODO
// DONE 1) Verify portaudio sound for known sample (e.g. 5Khz sine wave)
// N/A 2) Verify portaudio buffering of data sent to it
// DONE 3) Switch player to think in terms of generating audio samples and not passing of time
// 4) Figure out how to disable portaudio debug text

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Missing MOD filename")
	}

	wavOut := flag.String("wav", "", "output to a WAVE file")
	_ = wavOut
	flag.Parse()

	mod, err := ioutil.ReadFile(flag.Args()[0])
	if err != nil {
		panic(err)
	}

	var hdr ModHeader
	hdr.tempo = 125
	hdr.speed = 6

	buf := bytes.NewReader(mod)
	y := make([]byte, 20)
	buf.Read(y)
	hdr.title = string(y)

	initErr := portaudio.Initialize()
	defer func() {
		if initErr != nil {
			portaudio.Terminate()
		}
	}()

	outAudioBuf := make([]int16, outputBufferSamples)
	_ = outAudioBuf

	// Read sample information (sample data is read later)
	for i := 0; i < 31; i++ {
		s, err := readSampleInfo(buf)
		if err != nil {
			panic(err)
		}
		hdr.samples[i] = *s
	}

	// Read orders
	no, err := buf.ReadByte()
	hdr.nOrders = int(no)
	buf.ReadByte() // Discard
	_, err = buf.Read(hdr.orders[:])
	hdr.nPatterns = int(hdr.orders[0])
	for i := 1; i < 128; i++ {
		if int(hdr.orders[i]) > hdr.nPatterns {
			hdr.nPatterns = int(hdr.orders[i])
		}
	}

	// Detect number of channels
	x := make([]byte, 4)
	buf.Read(x)
	switch string(x[2:]) {
	case "K.": // M.K.
		hdr.nChannels = 4
	case "HN": // xCHN, x = number of channels
		hdr.nChannels = (int(x[0]) - 48)
	case "CH": // xxCH, xx = number of channels as two digit decimal
		hdr.nChannels = (int(x[0])-48)*10 + (int(x[1] - 48))
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

	player := NewPlayer(hdr, outputBufferHz)

	if *wavOut == "" {
		stream, err := portaudio.OpenDefaultStream(0, 2, float64(outputBufferHz), portaudio.FramesPerBufferUnspecified, player.audioCB)
		if err != nil {
			log.Fatal(err)
		}
		defer stream.Close()

		// fmt.Println("3")
		// time.Sleep(1 * time.Second)
		// fmt.Println("2")
		// time.Sleep(1 * time.Second)
		// fmt.Println("1")
		// time.Sleep(1 * time.Second)
		// fmt.Println("start")

		stream.Start()
		defer stream.Stop()

		<-player.endCh // wait for song to end
	} else {
		wavF, err := os.Create(*wavOut)
		if err != nil {
			log.Fatal(err)
		}
		defer wavF.Close()

		var wavW *wav.Writer
		if wavW, err = wav.NewWriter(wavF, outputBufferHz); err != nil {
			log.Fatal(err)
		}

		audioOut := make([]int16, 2048)

		playing := true
		go func() {
			for playing {
				pl := true

				select {
				case _ = <-player.endCh:
					pl = false
				default:
				}

				player.audioCB(audioOut)
				if err = wavW.WriteFrame(audioOut); err != nil {
					wavF.Close()
					log.Fatal(err)
				}
				playing = pl
			}
		}()

		// TODO: yuck! do something better
		for playing {
		}

		wavW.Finish()
		wavF.Close()
	}
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
		p.tick = p.speed

		pattern := int(p.hdr.orders[p.ordIdx])
		rowDataIdx := (pattern*64 + p.rowCounter) * 4 * p.hdr.nChannels

		fmt.Printf("%02X %02X|", p.ordIdx, p.rowCounter)

		for i := 0; i < p.hdr.nChannels; i++ {
			channel := &p.channels[i]

			channel.effectCounter = 0
			sampNum, period, effect, param := decodeNote(p.hdr.patterns[rowDataIdx : rowDataIdx+4])

			// Getting note triggering logic correct was a pain, H/T micromod

			// If there is an instrument/sample number then reset the volume
			// sample numbers are 1-based in MOD format
			if sampNum > 0 && sampNum < 32 {
				smp := &p.hdr.samples[sampNum-1]

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
					p.speed = int(param)
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
			if p.ordIdx >= p.hdr.nOrders {
				p.endCh <- struct{}{}
				// TODO: loop and reset instruments to default state
			}
		}
	} else {
		// channel tick
		for i := 0; i < p.hdr.nChannels; i++ {
			p.channelTick(&p.channels[i], i)
		}
	}
}

func (p *Player) generateAudio(out []int16, nSamples, offset int) {
	for s := offset * 2; s < (offset+nSamples)*2; s += 2 {
		out[s+0] = 0
		out[s+1] = 0
	}

	for chanIdx := range p.channels {
		channel := &p.channels[chanIdx]

		if channel.sampleIdx == -1 || channel.volume == 0 {
			continue
		}

		if (p.mute & (1 << chanIdx)) != 0 {
			continue
		}

		sample := &p.hdr.samples[channel.sampleIdx]
		if sample.length == 0 {
			continue
		}

		playbackHz := int(retraceNTSCHz / float32(channel.period*2))
		dr := uint(playbackHz<<16) / outputBufferHz
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

func (p *Player) audioCB(out []int16) {
	count := len(out) / 2 // portaudio counts L & R channels separately, length 2 means one stereo sample
	offset := 0
	for count > 0 {
		remain := p.samplesPerTick - p.tickSamplePos
		if remain > count {
			remain = count
		}

		p.generateAudio(out, remain, offset)
		offset += remain

		p.tickSamplePos += remain
		if p.tickSamplePos == p.samplesPerTick {
			p.sequenceTick()
			p.tickSamplePos = 0
		}
		count -= remain
	}
}
