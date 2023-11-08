package modplayer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	s3mfx_SetSpeed       = 0x1
	s3mfx_PatternJump    = 0x2
	s3mfx_PatternBreak   = 0x3
	s3mfx_TonePortamento = 0x7
	s3mfx_Special        = 0x13
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
	song.Tempo = int(header.Tempo)
	song.Speed = int(header.Speed)

	// Count up the number of channels
	var nc int
	for nc = 0; nc < 32; nc++ {
		if header.ChannelSettings[nc] == 255 {
			break
		}
	}
	song.Channels = nc

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

	song.patterns = make([][]note, header.NumPatterns)

	// Read in the packed pattern data
	for i := 0; i < int(header.NumPatterns); i++ {
		if _, err := buf.Seek(int64(paras[i+int(header.NumInstruments)])*16, io.SeekStart); err != nil {
			return nil, err
		}

		var packedLen int16
		if err := binary.Read(buf, binary.LittleEndian, &packedLen); err != nil {
			return nil, err
		}
		packedLen -= 2

		song.patterns[i] = initNotePattern(song.Channels)

		// TODO: What do we clear the pattern data to?

		row := 0
		for packedLen > 0 {
			b, err := buf.ReadByte()
			if err != nil {
				return nil, err
			}
			packedLen--
			if b == 0 {
				// End of row
				row++
				if row >= 64 {
					break
				}
				continue
			}

			chn := int(b & 31)
			if chn > song.Channels {
				// Bogus data, skip this packed byte. Need to use top 3 bits
				// of byte to determine how much data follows and needs to be
				// skipped. Since only 8 values, precomputed into small table.
				// top3 | skip
				//  000 |   0
				//  001 |   2
				//  010 |   1
				//  011 |   3
				//  100 |   2
				//  101 |   4
				//  110 |   3
				//  111 |   5
				skip := []int64{0, 2, 1, 3, 2, 4, 3, 5}[b>>5]
				buf.Seek(skip, io.SeekCurrent)
				packedLen -= int16(skip)
				continue
			}

			no := &song.patterns[i][row*song.Channels+chn]

			// note and instrument
			if b&32 == 32 {
				noter, _ := buf.ReadByte() // if noter < 254, hi nibble: octave, lo: note in octave
				intr, _ := buf.ReadByte()
				packedLen -= 2

				// Convert the S3M nibble note format into the internal player
				// note representation (but shifted up one octave).
				no.Pitch = playerNote(12 + 12*int(noter>>4) + int(noter&0xF))
				no.Sample = int(intr)
			}

			// volume
			if b&64 == 64 {
				vol, _ := buf.ReadByte()
				packedLen--
				no.Volume = int(vol)
			}

			// effect
			if b&128 == 128 {
				efct, _ := buf.ReadByte()
				parm, _ := buf.ReadByte()
				efct, parm = convertS3MEffect(efct, parm)
				no.Effect = efct
				no.Param = parm
				packedLen -= 2
			}
		}
	}

	return song, nil
}

func convertS3MEffect(efc, parm byte) (effect byte, param byte) {
	effect, param = efc, parm

	switch efc {
	case s3mfx_SetSpeed:
		effect = effectSetSpeed
	case s3mfx_PatternJump:
		effect = effectJumpToPattern
	case s3mfx_PatternBreak:
		effect = effectPatternBrk
	case s3mfx_TonePortamento:
		effect = effectPortaToNote
	case s3mfx_Special:
		if (parm >> 4) == 0xB {
			effect = effectPatternLoop
			param = param & 0xF
		}
	default:
		// no-op
	}

	return
}
