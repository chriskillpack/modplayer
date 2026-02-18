package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

const (
	scratchBufferSize = 10 * 1024
	audioBufferSize   = 756 / 2
	patternRowsBefore = 4
	patternRowsAfter  = 4
	uiLineCount       = 13
)

type displayMode int

const (
	displayModeWide displayMode = iota
	displayModeNarrow
	displayModeCompact
)

// AudioPlayer encapsulates audio playback and UI rendering
type AudioPlayer struct {
	player  *modplayer.Player
	reverb  comb.Reverber
	stream  *portaudio.Stream
	scratch []int16

	// UI state
	uiWriter        io.Writer
	selectedChannel int
	soloChannel     int
	lastState       modplayer.PlayerState
	displayMode     displayMode
	formatter       *noteFormatter

	// Lifecycle management
	ctx            context.Context
	cancelFn       context.CancelFunc
	wg             sync.WaitGroup
	stopOnce       sync.Once
	terminated     bool
	keyboardDoneCh chan struct{}
}

// noteFormatter handles formatting note data for display
type noteFormatter struct {
	mode displayMode
}

// NewAudioPlayer creates a new AudioPlayer instance
func NewAudioPlayer(player *modplayer.Player, reverb comb.Reverber, noUI bool) *AudioPlayer {
	var uiw io.Writer = os.Stdout
	if noUI {
		uiw = io.Discard
	}

	mode := determineDisplayMode(player.Song.Channels)
	ctx, cancel := context.WithCancel(context.Background())

	return &AudioPlayer{
		player:         player,
		reverb:         reverb,
		scratch:        make([]int16, scratchBufferSize),
		uiWriter:       uiw,
		soloChannel:    -1,
		displayMode:    mode,
		formatter:      &noteFormatter{mode: mode},
		ctx:            ctx,
		cancelFn:       cancel,
		keyboardDoneCh: make(chan struct{}),
	}
}

// Run starts the audio playback and UI rendering
func (ap *AudioPlayer) Run() error {
	if err := ap.Initialize(); err != nil {
		return err
	}

	if err := ap.setupAudioStream(); err != nil {
		return err
	}

	ap.setupSignalHandlers()
	ap.setupKeyboardHandlers()

	// Hide the cursor
	fmt.Fprint(ap.uiWriter, hideCursor)

	// Main render loop
	for {
		select {
		case <-ap.ctx.Done():
			goto exit
		default:
		}

		state := ap.player.State()

		if shouldUpdateUI(ap.lastState, state) {
			ap.renderUI(state)
			ap.lastState = state
		}
	}

exit:

	// Show the cursor
	fmt.Fprint(ap.uiWriter, showCursor)

	// Wait for keyboard listener to fully exit and restore terminal state
	select {
	case <-ap.keyboardDoneCh:
		// Keyboard cleanup completed
	case <-time.After(500 * time.Millisecond):
		// Timeout waiting for keyboard cleanup
	}

	ap.wg.Wait()
	return nil
}

// Initialize handles PortAudio initialization
func (ap *AudioPlayer) Initialize() error {
	return portaudio.Initialize()
}

// setupAudioStream creates and starts the audio stream
func (ap *AudioPlayer) setupAudioStream() error {
	stream, err := portaudio.OpenDefaultStream(
		0, 2,
		float64(*flagHz),
		audioBufferSize,
		ap.streamCallback,
	)
	if err != nil {
		return err
	}

	ap.stream = stream

	if err := stream.Start(); err != nil {
		stream.Close()
		return err
	}

	return nil
}

// streamCallback is called by PortAudio to generate audio samples
func (ap *AudioPlayer) streamCallback(out []int16) {
	sc := ap.scratch[:len(out)]

	if ap.player.IsPlaying() {
		ap.player.GenerateAudio(sc)
	} else {
		// Clear out the audio buffer to prevent unpleasant loops when
		// paused (we are still pushing PCM data to the audio device).
		clear(sc)
	}

	ap.reverb.InputSamples(sc)
	n := ap.reverb.GetAudio(out)

	if n == 0 {
		ap.player.Stop()
	}
}

// setupSignalHandlers handles OS signals like SIGINT
func (ap *AudioPlayer) setupSignalHandlers() {
	sigch := make(chan os.Signal, 5)
	signal.Notify(sigch, syscall.SIGINT)

	ap.wg.Add(1)
	go func() {
		defer ap.wg.Done()
		for {
			select {
			case <-ap.ctx.Done():
				return
			case sig := <-sigch:
				if sig == syscall.SIGINT {
					ap.Stop()
					return
				}
			}
		}
	}()
}

// setupKeyboardHandlers handles keyboard input
func (ap *AudioPlayer) setupKeyboardHandlers() {
	ap.wg.Add(1)
	go func() {
		defer ap.wg.Done()
		keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			// Check for immediate exit keys first
			if key.Code == keys.CtrlC || key.Code == keys.Escape {
				ap.Stop()
				return true, nil
			}

			// Handle other keys
			ap.handleKeyPress(key)

			return false, nil
		})
		// Signal that keyboard listener has fully exited
		close(ap.keyboardDoneCh)
	}()
}

// handleKeyPress processes a single key press
func (ap *AudioPlayer) handleKeyPress(key keys.Key) {
	switch key.Code {
	case keys.Left:
		ap.selectedChannel = max(ap.selectedChannel-1, 0)

	case keys.Right:
		ap.selectedChannel = min(ap.selectedChannel+1, ap.player.Song.Channels-1)

	case keys.Space:
		if ap.player.IsPlaying() {
			ap.player.Stop()
		} else {
			ap.player.Start()
		}

	case keys.RuneKey:
		if len(key.Runes) > 0 {
			switch key.Runes[0] {
			case 'q':
				ap.player.Mute = ap.player.Mute ^ (1 << ap.selectedChannel)

			case 's':
				if ap.soloChannel != ap.selectedChannel {
					ap.soloChannel = ap.selectedChannel
					ap.player.Mute = ^(1 << ap.selectedChannel)
				} else {
					ap.soloChannel = -1
					ap.player.Mute = 0
				}
			}
		}
	}
}

// Stop performs clean shutdown
func (ap *AudioPlayer) Stop() {
	ap.stopOnce.Do(func() {
		ap.player.Stop()
		ap.cancelFn()

		if ap.stream != nil {
			ap.stream.Stop()
			ap.stream.Close()
		}

		if !ap.terminated {
			portaudio.Terminate()
			ap.terminated = true
		}

		fmt.Fprint(ap.uiWriter, showCursor)
	})
}

// renderUI renders the complete UI
func (ap *AudioPlayer) renderUI(state modplayer.PlayerState) {
	ap.renderHeader(state)
	ap.renderInstrumentStatus(state)
	ap.renderChannelHeaders()
	ap.renderPatternRows(state)

	// Move cursor back to the top
	ncl := len(state.Channels) / 2
	fmt.Fprintf(ap.uiWriter, escape+"%dF", uiLineCount+ncl)
}

// renderHeader renders the title and playback info
func (ap *AudioPlayer) renderHeader(state modplayer.PlayerState) {
	song := ap.player.Song
	if len(song.Title) > 0 {
		fmt.Fprint(ap.uiWriter, song.Title+" ")
	}
	fmt.Fprintf(ap.uiWriter, "%s %02X/3F %s %02X/%02X %s %02d %s %3d\n",
		blue("row"), state.Row,
		blue("pat"), state.Order, len(song.Orders),
		blue("speed"), ap.player.Speed,
		blue("bpm"), ap.player.Tempo)
}

// renderInstrumentStatus shows which instruments are playing on each channel
func (ap *AudioPlayer) renderInstrumentStatus(state modplayer.PlayerState) {
	song := ap.player.Song
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
		fmt.Fprintf(ap.uiWriter, "%-32s", outs)
		if i&1 == 1 {
			fmt.Fprintln(ap.uiWriter)
		}
	}
	fmt.Fprintln(ap.uiWriter)
	fmt.Fprintln(ap.uiWriter)
}

// renderChannelHeaders renders the channel number headers
func (ap *AudioPlayer) renderChannelHeaders() {
	song := ap.player.Song
	fmt.Fprint(ap.uiWriter, "        ")
	for i := range min(song.Channels, 8) {
		const chanstr = "%2d       "
		if i == ap.selectedChannel {
			fmt.Fprint(ap.uiWriter, green(chanstr, i+1))
			continue
		}
		fmt.Fprintf(ap.uiWriter, chanstr, i+1)
	}
	fmt.Fprintln(ap.uiWriter)
}

// renderPatternRows renders the pattern data rows
func (ap *AudioPlayer) renderPatternRows(state modplayer.PlayerState) {
	for i := -patternRowsBefore; i <= patternRowsAfter; i++ {
		ap.renderNoteRow(state.Order, state.Row+i, i == 0)
	}
}

// renderNoteRow renders a single row of note data
func (ap *AudioPlayer) renderNoteRow(order, row int, isCurrent bool) {
	nd := ap.player.NoteDataFor(order, row)
	if nd == nil {
		fmt.Fprintln(ap.uiWriter)
		return
	}

	// Row prefix
	if isCurrent {
		fmt.Fprint(ap.uiWriter, ">>> ")
	} else {
		fmt.Fprint(ap.uiWriter, "    ")
	}

	// Note data for each channel
	maxChannels := 8
	if ap.displayMode == displayModeWide {
		maxChannels = 4
	}

	for ni, n := range nd {
		if ni >= maxChannels {
			if ni == maxChannels {
				fmt.Fprint(ap.uiWriter, " ...")
			}
			break
		}

		ap.formatter.formatNote(ni, n, ap.uiWriter)
	}

	// Row suffix
	if isCurrent {
		fmt.Fprint(ap.uiWriter, " <<<")
	}
	fmt.Fprintln(ap.uiWriter)
}

// formatNote formats and writes a single note to the writer
func (nf *noteFormatter) formatNote(ni int, n modplayer.ChannelNoteData, w io.Writer) {
	switch nf.mode {
	case displayModeWide:
		nf.formatWide(ni, n, w)
	case displayModeNarrow:
		nf.formatNarrow(ni, n, w)
	case displayModeCompact:
		nf.formatCompact(ni, n, w)
	}
}

// formatWide formats a note in wide display mode (shows all details)
func (nf *noteFormatter) formatWide(ni int, n modplayer.ChannelNoteData, w io.Writer) {
	fmt.Fprint(w, white("%s", n.Note), " ", cyan("%2X", n.Instrument), " ")
	if n.Volume != 0xFF {
		fmt.Fprint(w, green("%02X", n.Volume))
	} else {
		fmt.Fprint(w, green(".."))
	}
	fmt.Fprint(w, " ", magenta("%02X", n.Effect), yellow("%02X", n.Param))

	if ni < 3 {
		fmt.Fprint(w, "|")
	}
}

// formatNarrow formats a note in narrow display mode (omits instrument and volume)
func (nf *noteFormatter) formatNarrow(ni int, n modplayer.ChannelNoteData, w io.Writer) {
	fmt.Fprint(w, white("%s", n.Note), " ", magenta("%02X", n.Effect), yellow("%02X", n.Param))
	if ni < 7 {
		fmt.Fprint(w, "|")
	}
}

// formatCompact formats a note in compact display mode
func (nf *noteFormatter) formatCompact(ni int, n modplayer.ChannelNoteData, w io.Writer) {
	// Not implemented yet
}

// determineDisplayMode selects the appropriate display mode based on channel count
func determineDisplayMode(channels int) displayMode {
	if channels <= 4 {
		return displayModeWide
	} else if channels <= 8 {
		return displayModeNarrow
	}
	return displayModeNarrow
}

// shouldUpdateUI determines if the UI needs to be redrawn
func shouldUpdateUI(last, current modplayer.PlayerState) bool {
	if last.Notes == nil {
		return true
	}
	return last.Order != current.Order || last.Row != current.Row
}

// play is the original entry point, now a thin wrapper
func play(player *modplayer.Player, reverb comb.Reverber) {
	ap := NewAudioPlayer(player, reverb, *flagNoUI)

	// Ensure cleanup on any exit path
	defer func() {
		if ap.stream != nil {
			ap.stream.Stop()
			ap.stream.Close()
		}
		if !ap.terminated {
			portaudio.Terminate()
		}
		fmt.Fprint(ap.uiWriter, showCursor)
	}()

	if err := ap.Run(); err != nil {
		log.Fatal(err)
	}
}
