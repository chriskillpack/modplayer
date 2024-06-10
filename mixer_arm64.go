package modplayer

// #include "mixer_neon.h"
import "C"

var (
	d = make([]int8, 100)  // fake sample data
	o = make([]int16, 100) // fake audio output buffer
)

func mixChannelsNew(nSamples, offset int) {
	C.MixChannels_NEON((*C.short)(&o[0]), (*C.schar)(&d[0]), 0, 0, 0)
}
