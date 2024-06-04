#ifndef MIXER_NEON_H
#define MIXER_NEON_H

void MixChannels_NEON(int16_t* out, int8_t* data, uint8_t lvol, uint8_t rvol, int len);

#endif