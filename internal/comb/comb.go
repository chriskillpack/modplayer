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
