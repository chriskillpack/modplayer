// A very simple WAVE file writer
// Wrote my own after trying out a couple of others I found but
// both required me to know the quantity of audio data before I
// write it.
// See http://soundfile.sapp.org/doc/WaveFormat/ for format
// documentation.

package wav

import (
	"encoding/binary"
	"io"
)

const PCM = 1

type Writer struct {
	WS io.WriteSeeker
}

type Format struct {
	AudioFormat   uint16
	Channels      uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

// samples is N samples organized by channel
// [channel][sampleNum]samples
func (w *Writer) WriteFrame(samples [][]int16) error {
	for i := range samples[0] {
		s := [2]int16{samples[0][i], samples[1][i]}
		if err := binary.Write(w.WS, binary.LittleEndian, s); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) Finish() (int64, error) {
	wlen, err := w.WS.Seek(0, io.SeekCurrent)

	offset, err := w.WS.Seek(4, io.SeekStart)
	if offset != 4 || err != nil {
		return 0, err
	}
	if err := binary.Write(w.WS, binary.LittleEndian, int32(wlen-8)); err != nil {
		return 0, err
	}
	offset, err = w.WS.Seek(40, io.SeekStart)
	if offset != 40 || err != nil {
		return 0, err
	}
	if err := binary.Write(w.WS, binary.LittleEndian, int32(wlen-44)); err != nil {
		return 0, err
	}

	return wlen, nil
}

func NewWriter(ws io.WriteSeeker, sampleRate int) (*Writer, error) {
	writer := &Writer{WS: ws}

	if _, err := ws.Write([]byte("RIFF")); err != nil {
		return nil, err
	}

	// Write out zero for now, come back and fill this later
	if err := binary.Write(ws, binary.LittleEndian, int32(0)); err != nil {
		return nil, err
	}

	if _, err := ws.Write([]byte("WAVE")); err != nil {
		return nil, err
	}

	// Write format chunk
	if _, err := ws.Write([]byte("fmt ")); err != nil {
		return nil, err
	}
	if err := binary.Write(ws, binary.LittleEndian, int32(16)); err != nil {
		return nil, err
	}
	format := Format{AudioFormat: PCM, Channels: 2, SampleRate: uint32(sampleRate), BitsPerSample: 16}
	format.ByteRate = uint32(sampleRate) * 2 * (16 / 8)
	format.BlockAlign = 2 * (16 / 8)
	if err := binary.Write(ws, binary.LittleEndian, format); err != nil {
		return nil, err
	}

	// Write data chunk header
	if _, err := ws.Write([]byte("data")); err != nil {
		return nil, err
	}
	// Write out zero for the data size for now, come back and fill this later
	if err := binary.Write(ws, binary.LittleEndian, int32(0)); err != nil {
		return nil, err
	}

	return writer, nil
}
