package comb

// Comb models a simple Comb filter reverb module. At construction time it takes
// a block of sample data and applies reverb to it. It cannot be fed any more
// sample data after this.
type Comb struct {
	delayOffset int
	readPos     int
	audio       []int16
}

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

// How much unread data remains in the buffer. This does not account for any
// unprocessed reverb.
func (c *CombAdd) N() int {
	return len(c.audio) - c.readPos
}

// CombFixed is a Comb filter than uses a fixed size of backing memory
type CombFixed struct {
	Comb
	readPos, writePos int
	n                 int
	bufferSize        int
	decay             float32
}

// NewCombFixed creates a new Comb filter. The internal buffer is sized
// appropriately to support the desired reverb delay but it can be increased
// using the addSize parameter.
func NewCombFixed(addSize int, decay float32, delayMs, sampleRate int) *CombFixed {
	delayOffset := (delayMs * sampleRate) / 1000
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
	// fmt.Printf("IS s:%d n:%d r:%d w:%d l:%d", c.bufferSize, c.n, c.readPos, c.writePos, len(in))

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

	// Would adding this data exceed the end of the buffer?
	if c.writePos+n >= c.bufferSize {
		// Yes, do it in two parts (n1 to end of buffer, n2 the remainder)
		n1 := c.bufferSize - c.writePos
		n2 := n - n1
		copy(c.audio[c.writePos:c.writePos+n1], in[:n1])
		copy(c.audio[:n2], in[n1:n1+n2])
		// fmt.Printf(" split %d %d", n1, n2)
		c.writePos = n2
	} else {
		// fmt.Printf(" single")
		copy(c.audio[c.writePos:c.writePos+n], in[:n])
		c.writePos += n
	}
	c.n += n
	// fmt.Println()
	return n
}

func (c *CombFixed) GetAudio(out []int16) int {
	// fmt.Printf("GA s:%d n:%d r:%d w:%d l:%d", c.bufferSize, c.n, c.readPos, c.writePos, len(out))

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
		// fmt.Printf(" split %d %d", n1, n2)

		// TODO: Apply the reverb!
		c.readPos = n2
	} else {
		copy(out[:n], c.audio[c.readPos:c.readPos+n])

		// fmt.Printf(" single")
		// TODO: Apply the reverb!
		c.readPos += n
	}
	c.n -= n
	// fmt.Println()

	return n
}

// How much unread data remains in the buffer. This does not account for any
// unprocessed reverb.
func (c *CombFixed) N() int {
	return c.n
}
