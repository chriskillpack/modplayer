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

// combFilter implements a feedback comb filter - the core building block
// for reverb. It delays the input signal and feeds it back with a decay factor.
type combFilter struct {
	buffer      []int32
	bufferSize  int
	writePos    int
	decay       float32
	damping     float32 // one-pole lowpass coefficient for HF rolloff
	filterState int32   // state for the damping filter
}

// newCombFilter creates a new comb filter with the specified delay, decay, and damping.
// bufferSize is the delay in samples.
// damping controls high-frequency rolloff (0.0 = bright, 1.0 = very dark).
func newCombFilter(delay int, decay, damping float32) *combFilter {
	return &combFilter{
		buffer:      make([]int32, delay),
		bufferSize:  delay,
		writePos:    0,
		decay:       decay,
		damping:     damping,
		filterState: 0,
	}
}

// process applies the comb filter to a single sample.
// Implements a feedback comb filter with damping: buffer[pos] = input + decay*damped(delayed)
func (c *combFilter) process(input int32) int32 {
	// Read the delayed value from the current position
	delayed := c.buffer[c.writePos]

	// Apply one-pole lowpass filter for damping (simulates HF absorption in rooms)
	// filterState = damping*filterState + (1-damping)*delayed
	c.filterState = int32(float32(c.filterState)*c.damping + float32(delayed)*(1.0-c.damping))

	// Write new value: input + feedback (decayed and damped delayed signal)
	c.buffer[c.writePos] = input + int32(float32(c.filterState)*c.decay)

	// Advance write position in circular buffer
	c.writePos = (c.writePos + 1) % c.bufferSize

	// Output is the delayed signal
	return delayed
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

// StereoReverb implements a high-quality Schroeder reverb with multiple
// parallel comb filters per channel for denser, more natural-sounding reverb.
type StereoReverb struct {
	// Left and right channel comb filters (4 per channel)
	leftCombs  [4]*combFilter
	rightCombs [4]*combFilter

	// Left and right channel allpass filters (2 per channel)
	leftAllpass  [2]*allpassFilter
	rightAllpass [2]*allpassFilter

	// I/O ring buffer for processed audio
	audio      []int32
	bufferSize int
	readPos    int
	writePos   int
	n          int // samples currently stored

	// Configuration
	roomSize float32 // scales the decay of comb filters
	damping  float32 // high-frequency damping (0.0 = bright, 1.0 = dark)
}

// NewStereoReverb creates a new stereo reverb with 4 comb filters per channel.
// addSize provides extra buffer space beyond the minimum required for delays.
// roomSize ranges from 0.0-1.0 and controls the reverb tail length.
// damping ranges from 0.0-1.0 and controls high-frequency rolloff.
func NewStereoReverb(addSize int, roomSize, damping float32, sampleRate int) *StereoReverb {
	// Base delay times at 44.1kHz (in samples)
	// Left and right use different patterns for stereo width
	leftDelays := [4]int{1557, 1617, 1491, 1422}
	rightDelays := [4]int{1617, 1557, 1422, 1491}

	// Scale delays based on actual sample rate
	scaleDelay := func(baseDelay int) int {
		return (baseDelay * sampleRate) / 44100
	}

	// Calculate decay from roomSize: range 0.84 (small room) to 0.98 (large hall)
	decay := 0.84 + (roomSize * 0.14)

	s := &StereoReverb{
		roomSize: roomSize,
		damping:  damping,
	}

	// Initialize left channel combs
	for i := range s.leftCombs {
		delay := scaleDelay(leftDelays[i])
		s.leftCombs[i] = newCombFilter(delay, decay, damping)
	}

	// Initialize right channel combs
	for i := range s.rightCombs {
		delay := scaleDelay(rightDelays[i])
		s.rightCombs[i] = newCombFilter(delay, decay, damping)
	}

	// Allpass delays at 44.1kHz (in samples)
	allpassDelays := [2]int{556, 441}

	// Initialize left channel allpass filters
	for i := range s.leftAllpass {
		delay := scaleDelay(allpassDelays[i])
		s.leftAllpass[i] = newAllpass(delay)
	}

	// Initialize right channel allpass filters
	for i := range s.rightAllpass {
		delay := scaleDelay(allpassDelays[i])
		s.rightAllpass[i] = newAllpass(delay)
	}

	// Calculate I/O buffer size: largest delay + addSize, times 2 for stereo
	maxDelay := scaleDelay(1617) // largest delay time
	s.bufferSize = (maxDelay + addSize) * 2
	s.audio = make([]int32, s.bufferSize)

	return s
}

// InputSamples feeds stereo interleaved samples into the reverb.
// Returns the number of samples consumed.
func (s *StereoReverb) InputSamples(in []int16) int {
	// How much can the buffer take?
	free := s.bufferSize - s.n
	n := min(len(in), free)

	// If buffer is full, stop
	if n == 0 {
		return 0
	}

	// Process samples in pairs (L/R)
	numPairs := n / 2
	inPos := 0

	for i := 0; i < numPairs; i++ {
		// Deinterleave: extract left and right samples
		left := int32(in[inPos])
		right := int32(in[inPos+1])
		inPos += 2

		// Process left channel through 4 parallel combs and sum
		leftSum := int32(0)
		for j := range s.leftCombs {
			leftSum += s.leftCombs[j].process(left)
		}

		// Process right channel through 4 parallel combs and sum
		rightSum := int32(0)
		for j := range s.rightCombs {
			rightSum += s.rightCombs[j].process(right)
		}

		// Scale down to prevent overflow (8 combs total)
		// Divide by 4 gives headroom while maintaining good signal level
		leftOut := leftSum / 4
		rightOut := rightSum / 4

		// Pass through allpass filters for diffusion (left channel)
		for j := range s.leftAllpass {
			leftOut = s.leftAllpass[j].process(leftOut)
		}

		// Pass through allpass filters for diffusion (right channel)
		for j := range s.rightAllpass {
			rightOut = s.rightAllpass[j].process(rightOut)
		}

		// Write to ring buffer (interleaved)
		s.audio[s.writePos] = leftOut
		s.audio[s.writePos+1] = rightOut

		s.writePos += 2
		if s.writePos >= s.bufferSize {
			s.writePos = 0
		}
	}

	samplesConsumed := numPairs * 2
	s.n += samplesConsumed

	return samplesConsumed
}

// GetAudio retrieves processed audio with reverb applied.
// Returns the number of samples written to out.
func (s *StereoReverb) GetAudio(out []int16) int {
	n := min(len(out), s.n)

	// If buffer is empty, stop
	if n == 0 {
		return 0
	}

	// Handle circular buffer wraparound
	if s.readPos+n > s.bufferSize {
		n1 := s.bufferSize - s.readPos
		n2 := n - n1
		copyDownsample(out[:n1], s.audio[s.readPos:s.readPos+n1])
		copyDownsample(out[n1:n], s.audio[:n2])
		s.readPos = n2
	} else {
		copyDownsample(out[:n], s.audio[s.readPos:s.readPos+n])
		s.readPos += n
	}

	s.n -= n
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
