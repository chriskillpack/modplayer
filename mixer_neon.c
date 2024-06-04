//go:build cgo && (arm || arm64)

// Very much a WIP but here we are writing the beginnings of the mixer using
// NEON intrinsics. There is still work to be done around reading in sample
// data according to the playback rate. Currently the code just loads in four
// adjacent samples.

#include <stdio.h>
#include <string.h>
#include <arm_neon.h>
#include "mixer_neon.h"

void MixChannels_NEON(int16_t* out, int8_t* data, uint8_t lvol, uint8_t rvol, int len) {
    // Load in a hardcoded scale factor for Left mix (10), TODO: Right mix
    // vdup_n_u8 repeats the unsigned byte parameter into the bottom 8 bytes of the NEON register
    // vmovl_u8 converts the u8 to a u16 value by unpacking it, so ADAD.. becomes 00AD00AD...
    uint16x8_t scale_factor_l_16 = vmovl_u8(vdup_n_u8(lvol));
    uint16x8_t scale_factor_r_16 = vmovl_u8(vdup_n_u8(rvol));

    // This is a 'no-op' (TODO: confirm) that casts u16 values to s16. This is done to appease
    // the compiler because the sample data is signed and the multiply instruction can only do s x s.
    int16x8_t scale_factor_16_l_s = vreinterpretq_s16_u16(scale_factor_l_16);
    int16x8_t scale_factor_16_r_s = vreinterpretq_s16_u16(scale_factor_r_16);

    // TODO: This code assumes a mono output buffer, needs to handle stereo
    // For now we only write into the left channel
    int16x8x2_t interleaved_audio = vld2q_s16(out);

    // Load in 8 bytes of data
    int8x8_t data_load = vld1_s8(data);
    // Unpack into 16-bit words
    int16x8_t data_unpacked = vmovl_s8(data_load);
    // Multiply by the scale factor
    int16x8_t mul_l = vmulq_s16(data_unpacked, scale_factor_16_l_s);
    int16x8_t mul_r = vmulq_s16(data_unpacked, scale_factor_16_r_s);
    // Shift down by 8 to remove the fixed point factor
    int16x8_t mixed_l = vshrq_n_s16(mul_l, 8);
    int16x8_t mixed_r = vshrq_n_s16(mul_r, 8);

    // Perform saturated addition of the mixed values into the deinterleaved
    // output buffers.
    int16x8x2_t result;
    result.val[0] = vqaddq_s16(mixed_l, interleaved_audio.val[0]);
    result.val[1] = vqaddq_s16(mixed_r, interleaved_audio.val[1]);
    vst2q_s16(out, result);
}