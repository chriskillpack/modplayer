// A _very_ simple WAVE file writer
// Wrote my own after trying out a couple of others I found but
// both required me to know the quantity of audio data before I
// write it.
// See http://soundfile.sapp.org/doc/WaveFormat/ for format
// documentation.

package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const wavTypePCM = 1

// ErrInvalidChunkHeaderLength means that the provided letter chunk
// name was not 4 characters.
var ErrInvalidChunkHeaderLength = errors.New("Chunk header name is not 4 characters")

// A Writer writes a WAV file into WS
type Writer struct {
	WS io.WriteSeeker
}

type format struct {
	AudioFormat   uint16
	Channels      uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

// NewWriter returns a Writer that writes a WAV file and
// sample data to ws
func NewWriter(ws io.WriteSeeker, sampleRate int) (*Writer, error) {
	writer := &Writer{WS: ws}

	// Zero length for now, come back and fill this later
	if err := writer.writeChunkHeader("RIFF", 0); err != nil {
		return nil, err
	}

	if _, err := ws.Write([]byte("WAVE")); err != nil {
		return nil, err
	}

	// Write format chunk
	if err := writer.writeChunkHeader("fmt ", 16); err != nil {
		return nil, err
	}
	format := format{AudioFormat: wavTypePCM, Channels: 2, SampleRate: uint32(sampleRate), BitsPerSample: 16}
	format.ByteRate = uint32(sampleRate) * 2 * (16 / 8)
	format.BlockAlign = 2 * (16 / 8)
	if err := binary.Write(ws, binary.LittleEndian, format); err != nil {
		return nil, err
	}

	// Start audio data chunk
	if err := writer.writeChunkHeader("data", 0); err != nil {
		return nil, err
	}

	return writer, nil
}

// WriteFrame writes the provided interleaved stereo samples to
// w.
func (w *Writer) WriteFrame(samples []int16) error {
	return binary.Write(w.WS, binary.LittleEndian, samples)
}

// Finish must be called when all data has been written to the writer
// This allows the writer to update placeholders values with the correct
// values.
func (w *Writer) Finish() (int64, error) {
	wlen, _ := w.WS.Seek(0, io.SeekCurrent)
	fmt.Printf("!!! Finish is writing wlen %d bytes\n", wlen)

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

func (w *Writer) writeChunkHeader(chunk string, initialSize int) error {
	if len(chunk) != 4 {
		return ErrInvalidChunkHeaderLength
	}

	if n, err := w.WS.Write([]byte(chunk)); n != 4 || err != nil {
		return err
	}

	return binary.Write(w.WS, binary.LittleEndian, int32(initialSize))
}
