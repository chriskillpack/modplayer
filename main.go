package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"time"
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
}

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

const (
	effectPatternBrk = 0xD
	effectSetSpeed   = 0xF
)

func decodeNote(note []byte) (int, int, int, int) {
	sampNum := note[0]&0xF0 + note[2]>>4
	prdFreq := int(int(note[0]&0xF)<<8 + int(note[1]))
	effNum := note[2] & 0xF
	effParm := note[3]

	return int(sampNum), prdFreq, int(effNum), int(effParm)
}

func readSample(r *bytes.Reader) (*sample, error) {
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

type player struct {
	curSpeed   int
	lastSpeed  int
	rowCounter int
	orderIdx   int

	// timing stuff
	delay int
}

func newplayer(speed int) *player {
	p := &player{}
	p.curSpeed = speed
	return p
}

func (p *player) tick(t time.Time) {
}

func main() {
	mod, err := ioutil.ReadFile("space_debris.mod")
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

	// Read sample information
	for i := 0; i < 31; i++ {
		s, err := readSample(buf)
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

	songEndCh := make(chan int) // used to indicate end of song reached

	hz := (hdr.tempo * 2) / 5
	delay := time.Duration((1000 * time.Millisecond) / time.Duration(hz))
	fmt.Printf("delay %d\n", delay)

	player := newplayer(hdr.speed)

	fmt.Println("3")
	time.Sleep(1 * time.Second)
	fmt.Println("2")
	time.Sleep(1 * time.Second)
	fmt.Println("1")
	time.Sleep(1 * time.Second)
	fmt.Println("start")

	lastSpeed := 0
	ordIdx := 0
	rowCounter := 0
	curSpeed := hdr.speed

	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	var lastTick time.Time
	var myLastTick time.Time
	var tickAccumulator time.Duration

	go func() {
		for t := range ticker.C {
			player.tick(t)

			if myLastTick.IsZero() {
				myLastTick = t
			}
			tickTimeDelta := t.Sub(myLastTick)
			if tickTimeDelta < 0 {
				tickTimeDelta = 0
			}
			//fmt.Printf("tickTimeDelta %v\n", tickTimeDelta)
			if tickTimeDelta > 500*time.Millisecond {
				tickTimeDelta = 500 * time.Millisecond
			}
			myLastTick = t
			tickAccumulator += tickTimeDelta
			//fmt.Printf("tickAcc	%v\n", tickAccumulator)
			//if t.Sub(lastTick) > delay {
			if tickAccumulator > delay {
				//fmt.Printf("timeAccum %v\tdelay %v\n", tickAccumulator, delay)
				tickAccumulator -= delay

				if lastSpeed == 0 {
					pattern := int(hdr.orders[ordIdx])
					rowDataIdx := (pattern*64 + rowCounter) * 4 * hdr.nChannels
					fmt.Printf("%02X|", rowCounter)
					for i := 0; i < hdr.nChannels; i++ {
						sampNum, prdFreq, effNum, effParm := decodeNote(hdr.patterns[rowDataIdx : rowDataIdx+4])
						ni := periodToNote(prdFreq)
						fmt.Printf("%s %2X %X%02X", noteStr(ni), sampNum, effNum, effParm)
						if i < hdr.nChannels-1 {
							fmt.Print("|")
						}
						switch effNum {
						case effectSetSpeed:
							curSpeed = effParm
							break
						case effectPatternBrk:
							ordIdx++
							// TODO handle looping
							rowCounter = (effParm>>4)*10 + effParm&0xf
							// TODO skipping first row of pattern?
							break
						}
						rowDataIdx += 4
					}
					fmt.Println()
				} else {
					// This is where tick effects are processed
				}

				lastSpeed++
				if lastSpeed >= curSpeed {
					rowCounter++
					if rowCounter >= 64 {
						rowCounter = 0
						ordIdx++
						if ordIdx >= hdr.nOrders {
							close(songEndCh) // close channel to indicate end of song reached
						}
					}
					lastSpeed = 0
				}
				//fmt.Printf("%d\n", t.Sub(lastTick))
				lastTick = t
			}
		}
	}()

	_ = <-songEndCh // wait for song to end
}
