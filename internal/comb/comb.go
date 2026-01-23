package comb

// Reverber applies reverb to audio
type Reverber interface {
	// InputSamples feeds the reverber with new sample data.
	// Returns the number of samples that were consumed. Some implementations
	// may have an limit on the amount of audio data they will store.
	InputSamples(in []int16) int

	// Retrieve audio data with applied reverb. Returns the number of available
	// samples that were written to out. This may be less than the length of
	// out if there is limited processed audio available.
	GetAudio(out []int16) int
}

// CombAdd is a Comb filter can be fed audio data incrementally
// It does not discard used samples and has no upper bound on memory used
// Kept around as a clear implementation of reverb, not actually used anymore.
type CombAdd struct {
	readPos, writePos int
	delayOffset       int
	decay             float32
	audio             []int16
}

// NewCombAdd creates an instance of CombAdd
// initialSize is in sample pairs
func NewCombAdd(initialSize int, decay float32, delayMs, sampleRate int) *CombAdd {
	c := &CombAdd{
		delayOffset: (delayMs * sampleRate) / 1000,
		audio:       make([]int16, 0, initialSize*2),
		decay:       decay,
	}

	return c
}

// InputSamples feeds the CombAdd filter with new sample data. Once enough
// samples have been accumulated the filter will start applying reverb to audio
// data. The exact number of samples is determined by delay and sample rate.
// InputSamples returns the number of samples required before reverb can be
// applied. The functions takes a copy of the provided audio data.
func (c *CombAdd) InputSamples(in []int16) int {
	c.audio = append(c.audio, in...)
	if len(c.audio) > c.delayOffset*2 {
		ns := len(c.audio) - (c.delayOffset*2 + c.writePos)
		for i := range ns {
			c.audio[i+c.delayOffset*2+c.writePos] += int16(float32(c.audio[i+c.writePos]) * c.decay)
		}
		c.writePos += ns
	}
	rem := max(c.delayOffset*2-len(c.audio), 0)
	return rem
}

// GetAudio puts processed audio data into the out slice. It returns the number
// of samples put into out.
func (c *CombAdd) GetAudio(out []int16) int {
	wanted := len(out)
	have := len(c.audio) - c.readPos
	if wanted > have {
		wanted = have
	}
	if wanted > 0 {
		copy(out, c.audio[c.readPos:c.readPos+wanted])
		c.readPos += wanted
	}
	return wanted
}

// allpassFilter implements an allpass filter for diffusion in reverb.
// Allpass filters delay the signal and scatter reflections without
// changing the frequency content or amplitude.
type allpassFilter struct {
	buffer     []int32
	bufferSize int
	index      int
	gain       float32
}

// newAllpass creates a new allpass filter with the specified delay in samples.
func newAllpass(delay int) *allpassFilter {
	return &allpassFilter{
		buffer:     make([]int32, delay),
		bufferSize: delay,
		index:      0,
		gain:       0.5, // Fixed gain for stability
	}
}

// process applies the allpass filter to a single sample.
// Classic allpass formula: output = -input + delayed + gain*output_delayed
func (a *allpassFilter) process(input int32) int32 {
	// Read the delayed value from the buffer
	delayed := a.buffer[a.index]

	// Compute output: -input + delayed_input + gain*delayed_output
	// The delayed value in buffer already contains the feedback component
	output := -input + delayed

	// Write new value with feedback into the buffer
	a.buffer[a.index] = input + int32(float32(delayed)*a.gain)

	// Advance the circular buffer index
	a.index = (a.index + 1) % a.bufferSize

	return output
}

// CombFixed is a Comb filter than uses a fixed size of backing memory
type CombFixed struct {
	readPos, writePos     int
	n                     int
	seen                  int // how much has been seen, used for applying delay
	delayOffset, delayPos int
	bufferSize            int
	decay                 float32
	audio                 []int32
}

// NewCombFixed creates a new Comb filter. The internal buffer is sized
// appropriately to support the desired reverb delay but it can be increased
// using the addSize parameter.
func NewCombFixed(addSize int, decay float32, delayMs, sampleRate int) *CombFixed {
	delayOffset := (2 * delayMs * sampleRate) / 1000
	c := &CombFixed{
		audio:       make([]int32, (delayOffset+addSize)*2),
		delayOffset: delayOffset,
		bufferSize:  (delayOffset + addSize) * 2,
		decay:       decay,
	}
	return c
}

func (c *CombFixed) InputSamples(in []int16) int {
	// How much can the buffer take?
	free := c.bufferSize - c.n
	n := min(len(in), free)

	// If the buffer is full then stop
	if n == 0 {
		return 0
	}

	oldWritePos := c.writePos

	// Would adding this data exceed the end of the buffer?
	if c.writePos+n >= c.bufferSize {
		// Yes, do it in two parts (n1 to end of buffer, n2 the remainder)
		n1 := c.bufferSize - c.writePos
		n2 := n - n1
		copyUpsample(c.audio[c.writePos:c.writePos+n1], in[:n1])
		copyUpsample(c.audio[:n2], in[n1:n1+n2])
		c.writePos = n2
	} else {
		copyUpsample(c.audio[c.writePos:c.writePos+n], in[:n])
		c.writePos += n
	}
	c.n += n
	if c.seen+n >= c.delayOffset {
		if c.seen < c.delayOffset {
			// The written data partially straddles the delay offset, find out
			// where the offset falls in the written data
			off := c.delayOffset - c.seen

			// How much data needs to be processed?
			ns := (c.seen + n) - c.delayOffset

			// Apply the reverb
			c.applyReverb(ns, off)
		} else if oldWritePos+n < c.bufferSize {
			// Block fits entirely
			c.applyReverb(n, oldWritePos)
		} else {
			// Block straddles the buffer, split into two sections
			n1 := c.bufferSize - oldWritePos
			n2 := n - n1
			c.applyReverb(n1, oldWritePos)
			c.applyReverb(n2, 0)
		}
	}
	c.seen += n
	return n
}

func (c *CombFixed) applyReverb(ns, off int) {
	// Handle if the requested block wraps around the end of the buffer
	if c.delayPos+ns >= c.bufferSize {
		n1 := c.bufferSize - c.delayPos
		n2 := ns - n1

		// Pre-slice to avoid bounds checks in the loop
		dst := c.audio[off : off+n1]
		src := c.audio[c.delayPos : c.delayPos+n1]
		if len(src) > 0 {
			_ = dst[len(src)-1] // BCE hint: dst is at least as long as src
			for i, s := range src {
				dst[i] += int32(float32(s) * c.decay)
			}
		}

		// First part done, setup second part
		off += n1
		ns = n2
		c.delayPos = 0
	}

	// Pre-slice to avoid bounds checks in the loop
	dst := c.audio[off : off+ns]
	src := c.audio[c.delayPos : c.delayPos+ns]
	if len(src) > 0 {
		_ = dst[len(src)-1] // BCE hint: dst is at least as long as src
		for i, s := range src {
			dst[i] += int32(float32(s) * c.decay)
		}
	}
	c.delayPos += ns
}

func (c *CombFixed) GetAudio(out []int16) int {
	n := min(len(out), c.n)

	// If the buffer is empty then stop
	if n == 0 {
		return 0
	}

	if c.readPos+n > c.bufferSize {
		n1 := c.bufferSize - c.readPos
		n2 := n - n1
		copyDownsample(out[:n1], c.audio[c.readPos:c.readPos+n1])
		copyDownsample(out[n1:n], c.audio[:n2])

		c.readPos = n2
	} else {
		copyDownsample(out[:n], c.audio[c.readPos:c.readPos+n])

		c.readPos += n
	}
	c.n -= n

	return n
}

// Copies a slice of audio data and "upsamples" it to 32bit (just a cast, no
// value changes).
func copyUpsample(dst []int32, src []int16) {
	for i, s := range src {
		dst[i] = int32(s)
	}
}

// Copies a slice from the audio buffer to the output, clamping values to
// 16-bit signed range.
func copyDownsample(dst []int16, src []int32) {
	for i, s := range src {
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		dst[i] = int16(s)
	}
}
