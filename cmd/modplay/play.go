package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/internal/comb"
	"github.com/fatih/color"
	"github.com/gordonklaus/portaudio"
)

var (
	white   = color.New(color.FgWhite).SprintfFunc()
	cyan    = color.New(color.FgCyan).SprintfFunc()
	magenta = color.New(color.FgMagenta).SprintfFunc()
	yellow  = color.New(color.FgYellow).SprintfFunc()
	blue    = color.New(color.FgHiBlue).SprintFunc()
	green   = color.New(color.FgGreen).SprintfFunc()
)

const (
	escape     = "\x1b["
	hideCursor = escape + "?25l"
	showCursor = escape + "?25h"
)

type displayMode int

const (
	displayModeWide displayMode = iota
	displayModeNarrow
	displayModeCompact
)

func play(player *modplayer.Player, reverb comb.Reverber) {
	initErr := portaudio.Initialize()
	defer func() {
		if initErr != nil {
			portaudio.Terminate()
		}
	}()

	scratch := make([]int16, 10*1024)
	streamCB := func(out []int16) {
		sc := scratch[:len(out)]
		player.GenerateAudio(sc)
		reverb.InputSamples(sc)
		n := reverb.GetAudio(out)

		if n == 0 {
			player.Stop()
		}
	}

	stream, err := portaudio.OpenDefaultStream(0, 2, float64(*flagHz), 756/2, streamCB)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	stream.Start()
	defer stream.Stop()

	var uiw io.Writer = os.Stdout
	if *flagNoUI {
		uiw = io.Discard
	}

	stopFn := func() {
		player.Stop()
		stream.Stop()
		portaudio.Terminate()

		fmt.Fprintf(uiw, showCursor)
		os.Exit(0)
	}

	sigch := make(chan os.Signal, 5)
	signal.Notify(sigch, syscall.SIGINT)
	go func() {
		for {
			sig := <-sigch
			if sig == syscall.SIGINT {
				stopFn()
			}
		}
	}()

	song := player.Song

	// Hide the cursor
	fmt.Fprint(uiw, hideCursor)

	var mode displayMode
	if song.Channels <= 4 {
		mode = displayModeWide
	} else if song.Channels <= 8 {
		mode = displayModeNarrow
	} else {
		mode = displayModeNarrow /*displayModeCompact*/
	}

	// Print out some player preceeding 4 rows, current row and upcoming 4 rows
	// <title> row 1A/3F pat 0A/73 speed 6 bpm 125
	//
	//          0 0000|     0 0C00|^^.  0 0000|     0 0000
	//          0 0000|     0 0000|     0 0000|     0 0000
	//     C#5  F 0000|G-5 14 0000|     0 0000|     0 0000
	//          0 0000|     0 0000|     0 0000|     0 0000
	// >>>      0 0000|     0 0000|     0 0000|     0 0000 <<<
	//          0 0000|     0 0000|     0 0000|     0 0000
	//          0 0000|G-5 14 0C0B|     0 0000|     0 0000
	//          0 0000|     0 0000|     0 0000|     0 0000
	//     C#5  F 0000|     0 0000|     0 0000|     0 0000

	uiSelectedChannel := 0
	uiSoloChannel := -1

	go func() {
		keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			switch key.Code {
			case keys.CtrlC, keys.Escape:
				stopFn()
			case keys.Left:
				uiSelectedChannel = max(uiSelectedChannel-1, 0)
			case keys.Right:
				uiSelectedChannel = min(uiSelectedChannel+1, player.Song.Channels-1)
			case keys.RuneKey:
				if key.Runes[0] == 'q' {
					player.Mute = player.Mute ^ (1 << uiSelectedChannel)
				}
				if key.Runes[0] == 's' {
					if uiSoloChannel != uiSelectedChannel {
						uiSoloChannel = uiSelectedChannel
						player.Mute = ^(1 << uiSelectedChannel)
					} else {
						uiSoloChannel = -1
						player.Mute = 0
					}
				}
			}
			return false, nil
		})
	}()

	var lastState modplayer.PlayerState
	for player.IsPlaying() {
		state := player.State()

		if lastState.Notes != nil && lastState.Order == state.Order && lastState.Row == state.Row {
			continue
		}

		if len(song.Title) > 0 {
			fmt.Fprint(uiw, song.Title+" ")
		}
		fmt.Fprintf(uiw, "%s %02X/3F %s %02X/%02X %s %02d %s %3d\n", blue("row"), state.Row, blue("pat"), state.Order, len(song.Orders), blue("speed"), player.Speed, blue("bpm"), player.Tempo)

		// Print out which instrument channels are playing
		ncl := len(state.Channels) / 2
		for i, ch := range state.Channels {
			tc := ' '
			if state.Order == ch.TrigOrder && state.Row == ch.TrigRow {
				tc = '■'
			} else if ch.Instrument != -1 {
				tc = '□'
			}
			outs := fmt.Sprintf("%2d%c ", i+1, tc)

			si := ch.Instrument
			if si != -1 {
				outs += song.Samples[si].Name
			}
			fmt.Fprintf(uiw, "%-32s", outs)
			if i&1 == 1 {
				fmt.Fprintln(uiw)
			}
		}
		fmt.Fprintln(uiw)
		fmt.Fprintln(uiw)

		// Print the channel header
		fmt.Fprintf(uiw, "        ")
		for i := range min(song.Channels, 8) {
			const chanstr = "%2d       "
			if i == uiSelectedChannel {
				fmt.Fprint(uiw, green(chanstr, i+1))
				continue
			}
			fmt.Fprintf(uiw, chanstr, i+1)
		}
		fmt.Fprintln(uiw)

		for i := -4; i <= 4; i++ {
			nd := player.NoteDataFor(state.Order, state.Row+i)
			if nd == nil {
				fmt.Fprintln(uiw)
				continue
			}

			// If this is the currently playing row then highlight it
			if i == 0 {
				fmt.Fprint(uiw, ">>> ")
			} else {
				fmt.Fprint(uiw, "    ")
			}

			// Print out the first 4 channels of note data
			for ni, n := range nd {
				switch mode {
				case displayModeWide:
					noteDisplayWide(ni, n, uiw)
					if ni == 4 {
						break
					}
				case displayModeNarrow:
					noteDisplayNarrow(ni, n, uiw)
				case displayModeCompact:
					noteDisplayCompact(ni, n, uiw)
				}
			}
			if i == 0 {
				fmt.Fprint(uiw, " <<<")
			}
			fmt.Fprintln(uiw)
		}
		fmt.Fprintf(uiw, escape+"%dF", 13+ncl) // move cursor to beginning of line 9 above
	}

	// Show the cursor
	fmt.Fprintf(uiw, showCursor)
}

func noteDisplayWide(ni int, n modplayer.ChannelNoteData, uiw io.Writer) {
	if ni < 4 {
		fmt.Fprint(uiw, white("%s", n.Note), " ", cyan("%2X", n.Instrument), " ")
		if n.Volume != 0xFF {
			fmt.Fprint(uiw, green("%02X", n.Volume))
		} else {
			fmt.Fprint(uiw, green(".."))
		}
		fmt.Fprint(uiw, " ", magenta("%02X", n.Effect), yellow("%02X", n.Param))

		if ni < 3 {
			fmt.Fprint(uiw, "|")
		}
	} else if ni == 4 {
		fmt.Fprint(uiw, " ...")
	}

}

func noteDisplayNarrow(ni int, n modplayer.ChannelNoteData, uiw io.Writer) {
	if ni < 8 {
		fmt.Fprint(uiw, white("%s", n.Note), " ", magenta("%02X", n.Effect), yellow("%02X", n.Param))
		if ni < 7 {
			fmt.Fprint(uiw, "|")
		}
	} else if ni == 8 {
		fmt.Fprint(uiw, " ...")
	}
}

func noteDisplayCompact(ni int, n modplayer.ChannelNoteData, uiw io.Writer) {

}
