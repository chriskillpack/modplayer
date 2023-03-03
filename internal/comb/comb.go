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

// Comb models a simple Comb filter reverb module. At construction time it takes
// a block of sample data and applies reverb to it. It cannot be fed any more
// sample data after this.
type Comb struct {
	delayOffset int
	readPos     int
	audio       []int16
}

// NewComb creates an instance of Comb.
func NewComb(in []int16, decay float32, delayMs, sampleRate int) *Comb {
	c := &Comb{
		delayOffset: (delayMs * sampleRate) / 1000,
		audio:       make([]int16, len(in)),
	}

	copy(c.audio, in)
	for i := 0; i < len(in)/2-c.delayOffset; i++ {
		c.audio[(i+c.delayOffset)*2+0] += int16(float32(c.audio[i*2+0]) * decay)
		c.audio[(i+c.delayOffset)*2+1] += int16(float32(c.audio[i*2+1]) * decay)
	}

	return c
}

func (c *Comb) GetAudio(out []int16) int {
	n := len(out)
	if c.readPos+n > len(c.audio) {
		n = len(c.audio) - c.readPos
	}
	copy(out, c.audio[c.readPos:c.readPos+n])
	c.readPos += n
	return n
}

// CombAdd is a Comb filter can be fed audio data incrementally
// It does not discard used samples and has no upper bound on memory used
type CombAdd struct {
	Comb
	readPos  int
	writePos int
	decay    float32
}

// NewCombAdd creates an instance of CombAdd
// initialSize is in sample pairs
func NewCombAdd(initialSize int, decay float32, delayMs, sampleRate int) *CombAdd {
	c := &CombAdd{
		Comb: Comb{
			delayOffset: (delayMs * sampleRate) / 1000,
			audio:       make([]int16, 0, initialSize*2),
		},
		decay: decay,
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
		for i := 0; i < ns; i++ {
			c.audio[i+c.delayOffset*2+c.writePos] += int16(float32(c.audio[i+c.writePos]) * c.decay)
		}
		c.writePos += ns
	}
	rem := c.delayOffset*2 - len(c.audio)
	if rem < 0 {
		rem = 0
	}
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

// CombFixed is a Comb filter than uses a fixed size of backing memory
type CombFixed struct {
	Comb
	readPos, writePos int
	n                 int
	seen              int // how much has been seen, used for applying delay
	delayPos          int
	bufferSize        int
	decay             float32
}

// NewCombFixed creates a new Comb filter. The internal buffer is sized
// appropriately to support the desired reverb delay but it can be increased
// using the addSize parameter.
func NewCombFixed(addSize int, decay float32, delayMs, sampleRate int) *CombFixed {
	delayOffset := (2 * delayMs * sampleRate) / 1000
	c := &CombFixed{
		Comb: Comb{
			audio:       make([]int16, (delayOffset+addSize)*2),
			delayOffset: delayOffset,
		},
		bufferSize: (delayOffset + addSize) * 2,
		decay:      decay,
	}
	return c
}

func (c *CombFixed) InputSamples(in []int16) int {
	// How much can the buffer take?
	free := c.bufferSize - c.n
	n := len(in)
	if n > free {
		n = free
	}
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
		copy(c.audio[c.writePos:c.writePos+n1], in[:n1])
		copy(c.audio[:n2], in[n1:n1+n2])
		c.writePos = n2
	} else {
		copy(c.audio[c.writePos:c.writePos+n], in[:n])
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
		for i := 0; i < n1; i++ {
			c.audio[i+off] += int16(float32(c.audio[i+c.delayPos]) * c.decay)
		}

		// First part done, setup second part
		off += n1
		ns = n2
		c.delayPos = 0
	}

	for i := 0; i < ns; i++ {
		c.audio[i+off] += int16(float32(c.audio[i+c.delayPos]) * c.decay)
	}
	c.delayPos += ns
}

func (c *CombFixed) GetAudio(out []int16) int {
	n := len(out)
	if n > c.n {
		n = c.n
	}

	// If the buffer is empty then stop
	if n == 0 {
		return 0
	}

	if c.readPos+n > c.bufferSize {
		n1 := c.bufferSize - c.readPos
		n2 := n - n1
		copy(out[:n1], c.audio[c.readPos:c.readPos+n1])
		copy(out[n1:n], c.audio[:n2])

		c.readPos = n2
	} else {
		copy(out[:n], c.audio[c.readPos:c.readPos+n])

		c.readPos += n
	}
	c.n -= n

	return n
}
