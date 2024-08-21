package modplayer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	s3mfx_SetSpeed        = 0x1  // 'A'
	s3mfx_PatternJump     = 0x2  // 'B'
	s3mfx_PatternBreak    = 0x3  // 'C'
	s3mfx_VolumeSlide     = 0x4  // 'D'
	s3mfx_PortamentoDown  = 0x5  // 'E'
	s3mfx_PortamentoUp    = 0x6  // 'F'
	s3mfx_TonePortamento  = 0x7  // 'G'
	s3mfx_Vibrato         = 0x8  // 'H'
	s3mfx_SampleOffset    = 0xF  // 'O'
	s3mfx_Special         = 0x13 // 'S'
	s3mfx_SetTempo        = 0x14 // 'T'
	s3mfx_SetGlobalVolume = 0x16 // 'V'
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
	song.Title = cleanName(string(y))

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
		GlobalVolume    uint8
		Speed           uint8
		Tempo           uint8
		MasterVolume    uint8 // Bit 7 (1)=Stereo, (0)=Mono, ignore other bits
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
	song.GlobalVolume = int(header.GlobalVolume)

	// Count up the number of channels and build the channel remap table
	remap := make([]int, 32)
	song.Channels = 0
	for i := 0; i < 32; i++ {
		if header.ChannelSettings[i] < 16 {
			remap[song.Channels] = i
			song.Channels++
		}
	}
	dumpf("Name:\t\t%s\n", song.Title)
	dumpf("Channels:\t%d\n", song.Channels)
	dumpf("Speed:\t\t%d\n", song.Speed)
	dumpf("Tempo:\t\t%d\n", song.Tempo)

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
	dumpf("Orders:\t\t%d %v\n", len(song.Orders), song.Orders)

	// Load instrument and pattern parapointers
	paras := make([]uint16, int(header.NumInstruments)+int(header.NumPatterns))
	if err := binary.Read(buf, binary.LittleEndian, paras); err != nil {
		return nil, err
	}

	// Configure the channel default pan positions
	stereo := (header.MasterVolume & 128) == 128
	for i := 0; i < 32; i++ {
		if stereo {
			// In stereo, first 8 channels are left, next 8 are right. Last 16 are center
			if header.ChannelSettings[i] < 8 {
				song.pan[i] = 3 << 3
			} else if header.ChannelSettings[i] < 16 {
				song.pan[i] = 0xC << 3
			} else {
				song.pan[i] = 8 << 3 // "AdLib" channel, center pan
			}
		} else {
			song.pan[i] = 8 << 3 // mono song, pan position in the center
		}
	}

	if header.Panning == 0xFC {
		// Channel panning positions were provided, read them in
		var panning [32]byte
		if _, err := buf.Read(panning[:]); err != nil {
			return nil, err
		}
		for i := 0; i < 32; i++ {
			if panning[i]&0x20 == 0x20 {
				// Channel panning value provided use that
				song.pan[i] = (panning[i] & 0xF) << 3
			}
		}
	}
	dumpf("Pan:\t\t%v\n", song.pan)
	dumpf("Raw:\t\t%+v\n", header)

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
			Packing      byte   // should be 0
			Flags        byte   // bit 0 set if the sample is looping
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
			Name:      cleanName(string(instHeader.Name[:])),
			C4Speed:   int(instHeader.C2Speed),
			Volume:    int(instHeader.Volume),
		}
		// Some S3M instruments will have a loop length but not actually be a looping sample
		// The loop flag is the source of truth
		if instHeader.Flags&1 != 1 {
			sample.LoopLen = 0
		}

		dumpf("Instrument %d x%02X\n", i, i)
		dumpf("%s\n", sample)
		dumpf("\t%+v\n", *instHeader)

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
	noteDump := make([]note, song.Channels)

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

		dumpf("Pattern %d (x%02X)\n", i, i)

		row := 0
		for packedLen > 0 {
			b, err := buf.ReadByte()
			if err != nil {
				return nil, err
			}
			packedLen--
			if b == 0 {
				dumpf("%02X: %s\n", row, dumpRow(noteDump))

				// End of row
				row++
				if row >= 64 {
					break
				}
				continue
			}

			chn := remap[int(b&31)]
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
			nd := &noteDump[chn]

			// note and instrument
			if b&32 == 32 {
				noter, _ := buf.ReadByte() // if noter < 254, hi nibble: octave, lo: note in octave
				intr, _ := buf.ReadByte()
				packedLen -= 2

				// Convert the S3M nibble note format into the internal player
				// note representation (but shifted up one octave). Key off
				// 'notes' are passed through to player.
				switch noter {
				case noteKeyOff:
					no.Pitch = playerNote(noteKeyOff)
				case 255:
					// no note, only an instrument, mark the pitch as 0
					no.Pitch = 0
				default:
					no.Pitch = playerNote(12 + 12*int(noter>>4) + int(noter&0xF))
				}
				no.Sample = int(intr)

				nd.Pitch = no.Pitch
				nd.Sample = no.Sample
			}

			// volume
			if b&64 == 64 {
				vol, _ := buf.ReadByte()
				packedLen--
				no.Volume = int(vol)

				nd.Volume = no.Volume
			}

			// effect
			if b&128 == 128 {
				efct, _ := buf.ReadByte()
				parm, _ := buf.ReadByte()
				nd.Effect = efct
				nd.Param = parm

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
		// TODO: SetSpeed can take speed values above 0x20 which
		// effectSetSpeed currently interprets as setting tempo
		effect = effectSetSpeed
	case s3mfx_PatternJump:
		effect = effectJumpToPattern
	case s3mfx_PatternBreak:
		effect = effectPatternBrk
	case s3mfx_VolumeSlide:
		effect = effectS3MVolumeSlide
	case s3mfx_PortamentoUp:
		effect = effectS3MPortamentoUp
	case s3mfx_PortamentoDown:
		effect = effectS3MPortamentoDown
	case s3mfx_TonePortamento:
		effect = effectPortaToNote
	case s3mfx_Vibrato:
		effect = effectVibrato
	case s3mfx_SampleOffset:
		effect = effectSampleOffset
	case s3mfx_Special:
		switch parm >> 4 {
		case 0x8: // S8x Channel Pan Position
			effect = effectSetPanPosition
			param = (param & 0xF) << 3
		case 0xB: // SBx Pattern Loop
			effect = effectPatternLoop
			param = param & 0xF
		case 0xD: // SDx Note Delay
			effect = effectExtended
			param = (effectExtendedNoteDelay << 4) | param&0xF
		default:
			// Unhandled effects are disabled for now
			effect = 0
			param = 0
		}
	case s3mfx_SetTempo:
		effect = effectSetSpeed
	case s3mfx_SetGlobalVolume:
		effect = effectS3MGlobalVolume
	default:
		// disable the effect for now
		effect = 0
		param = 0
	}

	return
}

func dumpRow(row []note) string {
	var s string
	for i, no := range row {
		switch no.Pitch {
		case noteKeyOff:
			s += "^^..."
		case 0:
			s += "..."
		default:
			s += fmt.Sprintf("%s%d", notes[no.Pitch%12], no.Pitch/12-1)
		}
		switch no.Sample {
		case 0:
			s += ".."
		default:
			s += fmt.Sprintf("%02X", no.Sample)
		}

		if no.Volume != 0xFF {
			s += fmt.Sprintf("%02X", no.Volume)
		} else {
			s += ".."
		}
		if no.Effect != 0 {
			s += fmt.Sprintf("%c%02X", 'A'+(no.Effect-1), no.Param)
		} else {
			s += "..."
		}
		s += " "

		row[i] = note{}
	}

	return s
}
