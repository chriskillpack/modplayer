package modplayer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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

	// Count up the number of channels
	var nc int
	for nc = 0; nc < 32; nc++ {
		if header.ChannelSettings[nc] == 255 {
			break
		}
	}

	// Read in the orders
	orders := make([]byte, header.Length)
	if _, err := buf.Read(orders); err != nil {
		return nil, err
	}
	song.Orders = make([]byte, 0, header.Length)
	for _, pat := range orders {
		// We will keep the unused pattern marker in place (254)
		// if pat < 254 { // 254 means unused
		// 	song.Orders = append(song.Orders, pat)
		// }
		if pat == 255 { // 255 = end of song
			break
		}
		song.Orders = append(song.Orders, pat)
	}

	// Load instrument and pattern parapointers
	paras := make([]uint16, int(header.NumInstruments)+int(header.NumPatterns))
	if err := binary.Read(buf, binary.LittleEndian, paras); err != nil {
		return nil, err
	}

	// Read in the instrument sample data
	song.Samples = make([]Sample, int(header.NumInstruments))
	for i := 0; i < int(header.NumInstruments); i++ {
		if _, err := buf.Seek(int64(paras[i])*16, io.SeekStart); err != nil {
			return nil, err
		}
		instHeader := &struct {
			Type         byte
			Filename     [12]byte // Firelight doc has this as 13 bytes
			MemSegHi     byte
			MemSegLo     uint16
			SampleLength uint16
			_            uint16
			LoopBegin    uint16
			_            uint16
			LoopEnd      uint16
			_            uint16
			Volume       byte
			_            byte
			Packing      byte // should be 0
			Flags        byte
			C2Speed      uint16 // really this should be called C4Speed
			_            uint16
			_            [12]byte
			Name         [28]byte
			Scrs         [4]byte // 'SCRS'
		}{}
		if err := binary.Read(buf, binary.LittleEndian, instHeader); err != nil {
			return nil, err
		}
		if instHeader.Type > 1 {
			return nil, fmt.Errorf("unsupported sample type %d", instHeader.Type)
		}
		if instHeader.Flags&4 == 4 {
			return nil, fmt.Errorf("16-bit samples not currently supported")
		}

		sample := Sample{
			Length:    int(instHeader.SampleLength),
			LoopStart: int(instHeader.LoopBegin),
			LoopLen:   int(instHeader.LoopEnd) - int(instHeader.LoopBegin),
			Name:      strings.TrimRight(string(instHeader.Name[:]), "\x00"),
			C4Speed:   int(instHeader.C2Speed),
			Volume:    int(instHeader.Volume),
		}

		// Read sample data
		dataOffset := (uint(instHeader.MemSegHi)<<16 | uint(instHeader.MemSegLo)) * 16
		sample.Data = make([]int8, sample.Length)
		if sample.Length > 0 {
			if _, err := buf.Seek(int64(dataOffset), io.SeekStart); err != nil {
				return nil, err
			}
			if err := binary.Read(buf, binary.LittleEndian, sample.Data); err != nil {
				return nil, err
			}

			// Convert the unsigned S3M sample data to signed
			for j := range sample.Data {
				sample.Data[j] = int8(byte(sample.Data[j]) ^ 128)
			}
		}

		song.Samples[i] = sample
	}

	return nil, nil
}
