package modplayer

import "fmt"

// These are scalar mixing routines. In this context scalar means non-SIMD and
// implemented in Go.

// There are two mixing routines: one to mono mix a sample into the output
// buffer, the other is a stereo mix. A mono mix is a sample that is fully
// panned left or right. Currently the output buffer is int32 but this may
// change to int16 depending on how the NEON mixing implementation goes.

// These functions exist are still used by the NEON path to handle the trailing
// samples that are exact multiples of the NEON mixer batch size.
func mixChannelsMono_Scalar(pos, epos, dr, ns uint, cur, vol int, sample []int8, buffer []int) (uint, int) {
	var genns uint

	for pos < epos {
		sd := int(sample[pos>>16])
		buffer[cur] += sd * vol

		pos += dr
		cur += 2
		genns++
	}
	if genns != ns {
		fmt.Printf("mixChannelsMono_Scalar a:%d e:%d\n", genns, ns)
	}

	return pos, cur
}

func mixChannelsStereo_Scalar(pos, epos, dr, ns uint, cur, lvol, rvol int, sample []int8, buffer []int) (uint, int) {
	var genns uint

	for pos < epos {
		// WARNING: no clamping when mixing into mixbuffer. Clamping will be applied when the final audio is returned
		// to the caller.
		sd := int(sample[pos>>16])
		buffer[cur+0] += sd * lvol
		buffer[cur+1] += sd * rvol

		pos += dr
		cur += 2
		genns++
	}

	if genns != ns {
		fmt.Printf("mixChannelsStereo_Scalar a:%d e:%d\n", genns, ns)
	}

	return pos, cur
}
