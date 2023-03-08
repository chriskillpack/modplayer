package modplayer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
)

var ErrInvalidS3M = errors.New("invalid S3M file")

func NewS3MSongFromBytes(songBytes []byte) (*Song, error) {
	// Check if the song is an S3M
	if len(songBytes) < 48 || string(songBytes[44:48]) != "SCRM" {
		return nil, ErrInvalidS3M
	}

	song := &Song{}
	buf := bytes.NewReader(songBytes)
	y := make([]byte, 28)
	if _, err := buf.Read(y); err != nil {
		return nil, err
	}
	song.Title = strings.TrimRight(string(y), "\x00")

	header := struct {
		Pad             byte
		Filetype        byte
		_               uint16
		Length          uint16
		NumInstruments  uint16
		NumPatterns     uint16
		Flags           uint16
		Tracker         uint16
		SampleFormat    uint16  // 1 = signed, 2 = unsigned
		_               [4]byte // 'SCRM'
		Volume          uint8
		Speed           uint8
		Tempo           uint8
		MastVolume      uint8
		_               uint8
		Panning         uint8
		_               [8]byte
		_               [2]byte
		ChannelSettings [32]byte
	}{}
	if err := binary.Read(buf, binary.LittleEndian, &header); err != nil {
		return nil, err
	}

	return nil, nil
}
