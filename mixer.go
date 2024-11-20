//go:build !arm64

package modplayer

func mixChannelsMono(pos, epos, dr uint, cur, vol int, sample []int8, buffer []int) (uint, int) {
	return mixChannelsMono_Scalar(pos, epos, dr, cur, vol, sample, buffer)
}
func mixChannelsStereo(pos, epos, dr uint, cur, lvol, rvol int, sample []int8, buffer []int) (uint, int) {
	return mixChannelsStereo_Scalar(pos, epos, dr, cur, lvol, rvol, sample, buffer)
}
