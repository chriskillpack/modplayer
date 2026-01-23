package config

import (
	"fmt"

	"github.com/chriskillpack/modplayer/internal/comb"
)

// ReverbPassThrough implements comb.Reverber but does nothing to do the audio
// data.
type ReverbPassThrough struct {
	audio             []int16
	bufSize           int
	readPos, writePos int
	n                 int
}

var _ comb.Reverber = &ReverbPassThrough{}

// NewPassThrough creates a new instance of ReverbPassThrough
func NewPassThrough(bufferSize int) *ReverbPassThrough {
	return &ReverbPassThrough{
		audio:   make([]int16, bufferSize),
		bufSize: bufferSize,
	}
}

func (r *ReverbPassThrough) InputSamples(in []int16) int {
	// How much can the buffer take?
	free := r.bufSize - r.n
	n := len(in)
	if n > free {
		n = free
	}
	// If the buffer is full then stop
	if n == 0 {
		return 0
	}

	// Would adding this data exceed the end of the buffer?
	if r.writePos+n >= r.bufSize {
		// Yes, do it in two parts (n1 to end of buffer, n2 the remainder)
		n1 := r.bufSize - r.writePos
		n2 := n - n1
		copy(r.audio[r.writePos:r.writePos+n1], in[:n1])
		copy(r.audio[:n2], in[n1:n1+n2])
		r.writePos = n2
	} else {
		copy(r.audio[r.writePos:r.writePos+n], in[:n])
		r.writePos += n
	}
	r.n += n

	return n
}

func (r *ReverbPassThrough) GetAudio(out []int16) int {
	n := len(out)
	if n > r.n {
		n = r.n
	}

	// If the buffer is empty then stop
	if n == 0 {
		return 0
	}

	if r.readPos+n > r.bufSize {
		n1 := r.bufSize - r.readPos
		n2 := n - n1
		copy(out[:n1], r.audio[r.readPos:r.readPos+n1])
		copy(out[n1:n], r.audio[:n2])

		r.readPos = n2
	} else {
		copy(out[:n], r.audio[r.readPos:r.readPos+n])

		r.readPos += n
	}
	r.n -= n

	return n
}

// ReverbFromFlag initializes an instance of comb.Reverber according to the
// command line flag value.
func ReverbFromFlag(reverb string, sampleRate int) (r comb.Reverber, err error) {
	switch reverb {
	case "light":
		// Small room (bedroom/studio booth)
		r = comb.NewStereoReverb(10*1024, 0.5, 0.5, 0.3, sampleRate)
	case "medium":
		// Living room/small hall
		r = comb.NewStereoReverb(10*1024, 0.7, 0.6, 0.5, sampleRate)
	case "hall":
		// Concert hall
		r = comb.NewStereoReverb(10*1024, 0.9, 0.7, 0.7, sampleRate)
	case "none":
		// No reverb (passthrough)
		r = NewPassThrough(10 * 1024)
	default:
		err = fmt.Errorf("unrecognized reverb setting %q", reverb)
	}

	return r, err
}
