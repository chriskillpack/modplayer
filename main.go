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

	"github.com/gordonklaus/portaudio"
	"github.com/youpy/go-wav"
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
	data      []byte
}

type channel struct {
	sampleIdx      int
	period         int
	volume         int
	samplePosition uint

	effect byte
	param  byte
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

	channels []channel
}

func (p *Player) setTempo(tempo int) {
	p.samplesPerTick = ((p.samplingFrequency << 1) + (p.samplingFrequency >> 1)) / tempo
	p.tempo = tempo
}

func NewPlayer(song ModHeader, samplingFrequency int, endCh chan struct{}) *Player {
	player := &Player{samplingFrequency: samplingFrequency, hdr: song, speed: 6}
	player.setTempo(125)
	player.channels = make([]channel, song.nChannels)
	for i := 0; i < song.nChannels; i++ {
		player.channels[i].sampleIdx = -1
	}

	// TODO: signal end of song
	_ = endCh

	return player
}

const (
	outputBufferHz      = 44100
	outputBufferSamples = 8192
	retraceNTSCHz       = 7159090.5 // Amiga NTSC vertical retrace timing
	globalVolume        = 16        // Hack for now to boost volume

	// MOD note effects
	effectSampleOffset = 0x9
	effectVolumeSlide  = 0xA
	effectSetVolume    = 0xC
	effectPatternBrk   = 0xD
	effectSetSpeed     = 0xF
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
	if b > 7 {
		smp.fineTune = 16 - int(b)
	} else {
		smp.fineTune = int(b)
	}

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
	switch string(x) {
	case "M.K.":
		hdr.nChannels = 4
		break
	}

	// Read pattern data
	hdr.patterns = make([]byte, hdr.nChannels*(hdr.nPatterns+1)*64*4)
	buf.Read(hdr.patterns)

	// Read sample data
	for i := 0; i < 31; i++ {
		hdr.samples[i].data = make([]byte, hdr.samples[i].length)
		buf.Read(hdr.samples[i].data)
	}

	songEndCh := make(chan struct{}) // used to indicate end of song reached

	player := NewPlayer(hdr, outputBufferHz, songEndCh)

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

		_ = <-songEndCh // wait for song to end
	} else {
		wavF, err := os.Create(*wavOut)
		if err != nil {
			log.Fatal(err)
		}
		defer wavF.Close()

		ww := wav.NewWriter(wavF, 1000*1024, 2, outputBufferHz, 16)

		audioOut := make([][]int16, 2)
		for i := 0; i < 2; i++ {
			audioOut[i] = make([]int16, 1024)
		}

		// TODO: Get this working completely
		for f := 0; f < 1000; f++ {
			player.audioCB(audioOut)
			magic := make([]wav.Sample, 1024)
			for i := 0; i < 1024; i++ {
				magic[i].Values[0] = int(audioOut[0][i])
				magic[i].Values[1] = int(audioOut[1][i])
			}
			if err := ww.WriteSamples(magic); err != nil {
				log.Fatal(err)
			}
		}

	}
}

func (p *Player) channelTick(c *channel, ci int) {
	switch c.effect {
	case effectVolumeSlide:
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
}

func (p *Player) sequenceTick() {
	p.tick--
	if p.tick <= 0 {
		p.tick = p.speed

		pattern := int(p.hdr.orders[p.ordIdx])
		rowDataIdx := (pattern*64 + p.rowCounter) * 4 * p.hdr.nChannels

		fmt.Printf("%02X|", p.rowCounter)

		for i := 0; i < p.hdr.nChannels; i++ {
			channel := &p.channels[i]

			sampNum, period, effect, param := decodeNote(p.hdr.patterns[rowDataIdx : rowDataIdx+4])

			// If there is an instrument number then reset the volume
			if sampNum > 0 && sampNum < 32 {
				channel.sampleIdx = sampNum - 1 // sample numbers are 1-based in MOD format
				channel.volume = p.hdr.samples[sampNum-1].volume
			}
			// If there is a period...
			if period > 0 {
				// ... reset the period
				channel.period = period
				// ... and restart the sample if effect isn't 3, 5 or 0xEDx
				if effect != 3 && effect != 5 && !(effect == 0xE && param>>4 == 0xD) {
					channel.samplePosition = 0
				}
			}
			channel.effect = effect
			channel.param = param

			fmt.Printf("%s %2X %X%02X", noteStr(periodToNote(period)), sampNum, effect, param)
			if i < p.hdr.nChannels-1 {
				fmt.Print("|")
			}
			switch effect {
			case effectSetSpeed:
				p.speed = int(param)
			case effectSampleOffset:
				channel.samplePosition = uint(param) << 8
			case effectSetVolume:
				channel.volume = int(param)
			case effectPatternBrk:
				p.ordIdx++
				// TODO handle looping
				p.rowCounter = int((param>>4)*10 + param&0xF)
				// TODO skipping first row of pattern?
			}
			rowDataIdx += 4
		}
		fmt.Println()

		p.rowCounter++
		if p.rowCounter >= 64 {
			p.rowCounter = 0
			p.ordIdx++
			if p.ordIdx >= p.hdr.nOrders {
				p.ordIdx = 0
			}
		}
	} else {
		// channel tick
		for i := 0; i < p.hdr.nChannels; i++ {
			p.channelTick(&p.channels[i], i)
		}
	}
}

func (p *Player) generateAudio(out [][]int16, nSamples, offset int) {
	for s := offset; s < offset+nSamples; s++ {
		out[0][s] = 0
		out[1][s] = 0
	}

	for chanIdx := range p.channels {
		channel := &p.channels[chanIdx]

		if channel.sampleIdx == -1 {
			continue
		}

		sample := &p.hdr.samples[channel.sampleIdx]

		playbackHz := int(retraceNTSCHz / float32(channel.period*2))
		dr := uint(playbackHz<<16) / outputBufferHz
		pos := channel.samplePosition
		for off := offset; off < offset+nSamples; off++ {
			// WARNING: no clipping protection when mixing in the sample (hence the downshift)
			samp := (int16(sample.data[pos>>16]-128) * int16(channel.volume)) >> 2
			out[0][off] += samp
			out[1][off] += samp

			pos += dr
			if pos >= uint(sample.length<<16) {
				if sample.loopLen >= 0 {
					pos = uint(sample.loopStart) << 16
				} else {
					channel.sampleIdx = -1 // turn off the channel
				}
				break
			}
		}
		channel.samplePosition = pos
	}
}

func (p *Player) audioCB(out [][]int16) {
	count := len(out[0])
	// fmt.Println("count", count)
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
