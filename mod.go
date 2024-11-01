package modplayer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// This is the equivalent S3M C4Speed for the MOD finetune value
// This should be indexed by the byte value in the MOD file
// Taken from fs3mdoc.txt
var fineTuning = []int{
	8363, 8413, 8463, 8529, 8581, 8651, 8723, 8757,
	7895, 7941, 7985, 8046, 8107, 8169, 8232, 8280,
}

// NewMODSongFromBytes parses a MOD file into a Song.
//
// This means reading out instrument data, sample data, order
// and pattern data into structures that the Player can use.
func NewMODSongFromBytes(songBytes []byte) (*Song, error) {
	song := &Song{
		Speed:        6,
		Tempo:        125,
		GlobalVolume: maxVolume,
		Samples:      make([]Sample, 31),
	}

	buf := bytes.NewReader(songBytes)
	y := make([]byte, 20)
	buf.Read(y)
	song.Title = cleanName(string(y))

	// Read sample information (sample data is read later)
	for i := 0; i < 31; i++ {
		s, err := readMODSampleInfo(buf, i)
		if err != nil {
			return nil, err
		}
		song.Samples[i] = *s
	}

	// Read orders
	orders := struct {
		Orders    uint8
		_         uint8
		OrderData [128]byte
	}{}

	if err := binary.Read(buf, binary.BigEndian, &orders); err != nil {
		return nil, err
	}
	song.Orders = make([]byte, orders.Orders)
	copy(song.Orders, orders.OrderData[:orders.Orders])

	// Detect number of patterns by finding maximum pattern id in song
	// orders table.
	patterns := int(song.Orders[0])
	for i := 1; i < 128; i++ {
		if int(orders.OrderData[i]) > patterns {
			patterns = int(orders.OrderData[i])
		}
	}
	patterns++ // num patterns = max_pattern_idx + 1

	// Detect number of channels from MOD signature
	// Errors if signature not recognized
	x := make([]byte, 4)
	if n, err := buf.Read(x); n != 4 || err != nil {
		return nil, err
	}
	switch string(x[2:]) {
	case "K.": // M.K.
		song.Channels = 4
	case "HN": // xCHN, x = number of channels
		song.Channels = (int(x[0]) - 48)
	case "CH": // xxCH, xx = number of channels as two digit decimal
		song.Channels = (int(x[0])-48)*10 + (int(x[1] - 48))
	default:
		return nil, fmt.Errorf("unrecognized MOD format %s", string(x))
	}
	dumpf("Title:\t\t%s\n", song.Title)
	dumpf("Channels:\t%d\n", song.Channels)
	dumpf("Speed:\t\t%d\n", song.Speed)
	dumpf("Tempo:\t\t%d\n", song.Tempo)
	dumpf("Patterns:\t%d\n", patterns)
	dumpf("Orders:\t\t%d %v\n", len(song.Orders), song.Orders)
	dumpf("\n")

	// Setup panning
	for i := 0; i < song.Channels; i++ {
		switch i & 3 {
		case 0, 3:
			song.pan[i] = 0 // left
		case 1, 2:
			song.pan[i] = 127 // right
		}
	}

	const bytesPerChannel = 4

	// Read pattern data
	song.patterns = make([][]note, patterns)
	scratch := make([]byte, rowsPerPattern*song.Channels*bytesPerChannel)
	for i := 0; i < patterns; i++ {
		song.patterns[i] = make([]note, rowsPerPattern*song.Channels)
		if n, err := buf.Read(scratch); n != rowsPerPattern*song.Channels*bytesPerChannel || err != nil {
			return nil, err
		}

		dumpf("Pattern %d (x%02X)\n", i, i)
		for p := 0; p < rowsPerPattern*song.Channels; p++ {
			n := noteFromMODbytes(scratch[p*bytesPerChannel : (p+1)*bytesPerChannel])

			if dumpW != nil {
				row := p / song.Channels
				ch := p % song.Channels
				if ch == 0 {
					dumpf("%02X: ", row)
				}

				data := dumpNoteFromMODbytes(scratch[p*bytesPerChannel : (p+1)*bytesPerChannel])
				dumpf("%4d", data[0])
				if data[0] == 0 {
					dumpf(".....")
				} else {
					dumpf("(%s)", noteStrFromPeriod(data[0]))
				}
				dumpf("%02X %X%02X", data[1], data[2], data[3])

				if ch == song.Channels-1 {
					dumpf("\n")
				}
			}

			if n.Effect == effectSetVolume {
				n.Volume = int(n.Param)
			} else {
				n.Volume = 0xFF // no volume set on this note
			}

			if n.Effect == effectExtended && (n.Param>>4 == effectExtendedNoteRetrig) {
				n.Effect = effectNoteRetrigVolSlide
				n.Param = n.Param & 0xF
			}

			song.patterns[i][p] = n
		}
		dumpf("\n")
	}

	// Read sample data
	for i := 0; i < 31; i++ {
		// Some MOD files store a sample length longer than what remains in the
		// buffer, e.g. believe.mod sample index 8 has a recorded length of 2358 but
		// only 2353 bytes remain in the file. binary.Read will return EOF and not read
		// anything in this situation, so read in the max available.
		n := song.Samples[i].Length
		if n > buf.Len() {
			n = buf.Len()
		}

		song.Samples[i].Data = make([]int8, song.Samples[i].Length)
		err := binary.Read(buf, binary.LittleEndian, song.Samples[i].Data[0:n])
		if err != nil {
			return nil, err
		}
		song.Samples[i].Length = n
	}

	return song, nil
}

func readMODSampleInfo(r *bytes.Reader, si int) (*Sample, error) {
	data := struct {
		Name      [22]byte
		Length    uint16
		FineTune  uint8
		Volume    uint8
		LoopStart uint16
		LoopLen   uint16
	}{}

	if err := binary.Read(r, binary.BigEndian, &data); err != nil {
		return nil, err
	}
	dumpf("Sample %d x%02X\n", si, si)

	smp := &Sample{
		Name:      cleanName(string(data.Name[:])),
		Length:    int(data.Length) * 2,
		C4Speed:   fineTuning[data.FineTune],
		Volume:    int(data.Volume),
		LoopStart: int(data.LoopStart) * 2,
		LoopLen:   int(data.LoopLen) * 2,
	}
	if smp.LoopLen < 4 {
		smp.LoopLen = 0
	}

	// If the loop data overshoots the end of the sample then correct the loop
	// This logic lifted from MilkyTracker, not encountered these situations yet
	if smp.LoopStart+smp.LoopLen > smp.Length {
		// First attempt, move the loop start back
		dx := smp.LoopStart + smp.LoopLen - smp.Length
		smp.LoopStart -= dx
		// If it still overshoots the end then clamp the loop
		if smp.LoopStart+smp.LoopLen > smp.Length {
			dx = smp.LoopStart + smp.LoopLen - smp.Length
			smp.LoopLen -= dx
		}
	}
	if smp.LoopLen < 2 {
		smp.LoopLen = 0
	}
	dumpf("%s\n", smp)
	dumpf("\t%+v\n", data)

	return smp, nil
}

func noteFromMODbytes(nb []byte) note {
	period := int(int(nb[0]&0xF)<<8 + int(nb[1])) // This is an Amiga MOD period

	return note{
		Sample: int(nb[0]&0xF0 + nb[2]>>4),
		Pitch:  periodToPlayerNote(period),
		Effect: nb[2] & 0xF,
		Param:  nb[3],
	}
}

// returned slice
//
//	0: Period
//	1: Sample
//	2: Effect
//	3: Param
func dumpNoteFromMODbytes(nb []byte) []int {
	return []int{
		int(int(nb[0]&0xF)<<8 + int(nb[1])),
		int(nb[0]&0xF0 + nb[2]>>4),
		int(nb[2] & 0xF),
		int(nb[3]),
	}
}

// Strips trailing 0x00 bytes and replaces any non ASCII character with a space
func cleanName(in string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r > 127 {
			return ' '
		}
		return r
	}, strings.TrimRight(in, "\x00"))
}

const (
	periodBase = 13696                                  // the amiga MOD period value for C-(-1), it's -1 in the octave numbering system we use
	ln2        = 0.693147180559945309417232121458176568 // ln(2)
)

// Convert an Amiga MOD period value to the octave*12+note format used
// internally in the player. This code is a complete lift from libxmp.
func periodToPlayerNote(period int) playerNote {
	if period <= 0 {
		return 0
	}

	// Some music theory - A4 is 440Hz, A5 is 880Hz and A3 is 220Hz. Each octave
	// is a power of 2 apart. An octave consists of 12 semitones, and each
	// semitone is separated by a gap of 2^1/12 (≅1.0595). A4=440Hz,
	// A#4=466Hz (440*1.0595), B4=493Hz (466*1.0595).
	// MOD format - ProTracker MOD format uses period values that it divides a
	// constant by to compute the instrument sample playback speed. The period
	// value for A4 is 254, A#4=240, A3=508 and A5=127. You can see that these
	// same relationships from musical theory hold.
	//
	// With these properties we can derive an equation that converts the MOD
	// periods to what (I think) Trackers call "linear" notes (technically they
	// are only linear wrt to the exponent), e.g. 440*2^(1+1/12) = A#5 and
	// 440*2^(-1/12)=G#4.
	calc := 12.0 * math.Log(float64(periodBase)/float64(period)) / ln2

	// libxmp added 1 to the return value but then took it off somewhere else in
	// the player so we drop that for now.
	return playerNote(math.Floor(calc + 0.5))
}
