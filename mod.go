package modplayer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// NewMODSongFromBytes parses a MOD file into a Song.
//
// This means reading out instrument data, sample data, order
// and pattern data into structures that the Player can use.
func NewMODSongFromBytes(songBytes []byte) (*Song, error) {
	song := &Song{
		Speed:   6,
		Tempo:   125,
		Samples: make([]Sample, 31),
	}

	buf := bytes.NewReader(songBytes)
	y := make([]byte, 20)
	buf.Read(y)
	song.Title = strings.TrimRight(string(y), "\x00")

	// Read sample information (sample data is read later)
	for i := 0; i < 31; i++ {
		s, err := readMODSampleInfo(buf)
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

	const bytesPerChannel = 4

	// Read pattern data
	song.patterns = make([][]note, patterns)
	scratch := make([]byte, rowsPerPattern*song.Channels*bytesPerChannel)
	for i := 0; i < patterns; i++ {
		song.patterns[i] = make([]note, rowsPerPattern*song.Channels)
		if n, err := buf.Read(scratch); n != rowsPerPattern*song.Channels*bytesPerChannel || err != nil {
			return nil, err
		}

		for p := 0; p < rowsPerPattern*song.Channels; p++ {
			song.patterns[i][p] = noteFromMODbytes(scratch[p*bytesPerChannel : (p+1)*bytesPerChannel])
		}
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

func readMODSampleInfo(r *bytes.Reader) (*Sample, error) {
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

	smp := &Sample{
		Name:      strings.TrimRight(string(data.Name[:]), "\x00"),
		Length:    int(data.Length) * 2,
		FineTune:  int(data.FineTune&7) - int(data.FineTune&8) + 8,
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

	return smp, nil
}

func noteFromMODbytes(nb []byte) note {
	return note{
		Sample: int(nb[0]&0xF0 + nb[2]>>4),
		Period: int(int(nb[0]&0xF)<<8 + int(nb[1])),
		Effect: nb[2] & 0xF,
		Param:  nb[3],
	}
}
