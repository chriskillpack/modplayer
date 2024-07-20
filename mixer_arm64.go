package modplayer

// #include "mixer_neon.h"
import "C"

var (
	d = make([]int8, 100)  // fake sample data
	o = make([]int16, 100) // fake audio output buffer
)

func mixChannelsMono(pos, epos, dr, ns uint, cur, vol int, sample []int8, buffer []int) (uint, int) {
	return mixChannelsMono_Scalar(pos, epos, dr, ns, cur, vol, sample, buffer)
}

func mixChannelsStereo(pos, epos, dr, ns uint, cur, lvol, rvol int, sample []int8, buffer []int) (uint, int) {
	// C.MixChannels_NEON((*C.short)(&o[0]), (*C.schar)(&d[0]), 0, 0, 0)
	return mixChannelsStereo_Scalar(pos, epos, dr, ns, cur, lvol, rvol, sample, buffer)
}
