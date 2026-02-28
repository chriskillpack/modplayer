package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/internal/comb"
	"github.com/fatih/color"
	"github.com/gordonklaus/portaudio"
	"golang.org/x/term"
)

var (
	cyan    = color.New(color.FgCyan).SprintfFunc()
	magenta = color.New(color.FgMagenta).SprintfFunc()
	yellow  = color.New(color.FgYellow).SprintfFunc()
	blue    = color.New(color.FgHiBlue).SprintfFunc()
	green   = color.New(color.FgGreen).SprintfFunc()
)

const (
	escape      = "\x1b["
	hideCursor  = escape + "?25l"
	showCursor  = escape + "?25h"
	clearScreen = escape + "2J"
	homePos     = escape + "H"

	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

const (
	scratchBufferSize = 10 * 1024
	audioBufferSize   = 756 / 2
	patternRowsBefore = 4
	patternRowsAfter  = 4
	uiLineCount       = 15
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

	audioRMS atomic.Uint64

	// UI state
	uiWriter        io.Writer
	selectedChannel int
	soloChannel     int
	lastState       modplayer.PlayerState
	lastUIUpdate    time.Time
	displayMode     displayMode
	formatter       *noteFormatter
	termWidth       int
	layout          channelLayout

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

// channelLayout describes how to display channels on screen
type channelLayout struct {
	displayMode    displayMode
	channelWidth   int // Width in characters per channel
	maxChannels    int // Maximum channels that can fit
	separatorWidth int // Width of separator between channels
}

// NewAudioPlayer creates a new AudioPlayer instance
func NewAudioPlayer(player *modplayer.Player, reverb comb.Reverber, noUI bool) *AudioPlayer {
	var uiw io.Writer = os.Stdout
	if noUI {
		uiw = io.Discard
	}

	// Get terminal width, default to 80 if unable to determine
	width := 80
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		if w, _, err := term.GetSize(fd); err == nil && w > 0 {
			width = w
		}
	}

	layout := computeChannelLayout(player.Song.Channels, width)
	ctx, cancel := context.WithCancel(context.Background())

	return &AudioPlayer{
		player:         player,
		reverb:         reverb,
		scratch:        make([]int16, scratchBufferSize),
		uiWriter:       uiw,
		soloChannel:    -1,
		displayMode:    layout.displayMode,
		formatter:      &noteFormatter{mode: layout.displayMode},
		termWidth:      width,
		layout:         layout,
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

	// Clear screen, move to home position, and hide cursor
	fmt.Fprint(ap.uiWriter, clearScreen, homePos, hideCursor)

	// Main render loop
	ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS for power meter
	defer ticker.Stop()

	for {
		select {
		case <-ap.ctx.Done():
			goto exit
		case <-ticker.C:
			state := ap.player.State()
			stateChanged := shouldUpdateUI(ap.lastState, state)

			// Save cursor position before rendering
			fmt.Fprint(ap.uiWriter, escape+"s")

			// Always render header and power meter for smooth updates
			ap.renderHeader(state)
			ap.renderPowerMeter()

			// Only render the rest when state changes
			if stateChanged {
				ap.renderInstrumentStatus(state)
				ap.renderChannelHeaders()
				ap.renderPatternRows(state)
				ap.lastState = state
			}

			// Restore cursor position after rendering
			fmt.Fprint(ap.uiWriter, escape+"u")
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

	ap.computeRMS(sc)

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

// renderUI renders the complete UI (used for initial render)
func (ap *AudioPlayer) renderUI(state modplayer.PlayerState) {
	ap.renderHeader(state)
	ap.renderPowerMeter()
	ap.renderInstrumentStatus(state)
	ap.renderChannelHeaders()
	ap.renderPatternRows(state)

	// Move cursor back to the top
	ncl := len(state.Channels) / 2
	fmt.Fprintf(ap.uiWriter, escape+"%dF", uiLineCount+ncl)
}

func (ap *AudioPlayer) computeRMS(lraudio []int16) {
	var sumL, sumR float64
	for i := 0; i < len(lraudio); i += 2 {
		l := float64(lraudio[i])
		r := float64(lraudio[i+1])
		sumL += l * l
		sumR += r * r
	}
	n := float64(len(lraudio) / 2)
	rmsL := float32(math.Sqrt(sumL / n))
	rmsR := float32(math.Sqrt(sumR / n))

	bl := math.Float32bits(rmsL)
	br := math.Float32bits(rmsR)
	ap.audioRMS.Store(uint64(bl)<<32 | uint64(br))
}

// renderHeader renders the title and playback info
func (ap *AudioPlayer) renderHeader(state modplayer.PlayerState) {
	song := ap.player.Song

	// Build the header text without colors first to calculate visible length
	var plainText strings.Builder
	if len(song.Title) > 0 {
		plainText.WriteString(song.Title)
		plainText.WriteString(" ")
	}
	plainText.WriteString(fmt.Sprintf("row %02X/3F pat %02X/%02X speed %02d bpm %3d",
		state.Row,
		state.Order, len(song.Orders),
		ap.player.Speed,
		ap.player.Tempo))

	// Build the colored version for display
	var headerText strings.Builder
	if len(song.Title) > 0 {
		headerText.WriteString(song.Title)
		headerText.WriteString(" ")
	}
	headerText.WriteString(fmt.Sprintf("%s %02X/3F %s %02X/%02X %s %02d %s %3d",
		blue("row"), state.Row,
		blue("pat"), state.Order, len(song.Orders),
		blue("speed"), ap.player.Speed,
		blue("bpm"), ap.player.Tempo))

	// Calculate padding for centering using the plain text length
	headerLen := len(plainText.String())
	padding := (ap.termWidth - headerLen) / 2
	if padding < 0 {
		padding = 0
	}

	fmt.Fprintf(ap.uiWriter, "%*s%s\n", padding, "", headerText.String())
}

func colorForDb(db float64) string {
	if db > -6 {
		return colorGreen
	} else if db > -18 {
		return colorYellow
	}
	return colorRed
}

func renderPowerMeterHalf(filled, halfWidth int, minDb float64) string {
	var sb strings.Builder
	currentColor := ""
	for i := range halfWidth {
		posDB := minDb + (float64(i)/float64(halfWidth))*(-minDb)
		if i < halfWidth-filled {
			if currentColor != "" {
				sb.WriteString(colorReset)
				currentColor = ""
			}
			sb.WriteString("·")
		} else {
			c := colorForDb(posDB)
			if c != currentColor {
				sb.WriteString(c)
				currentColor = c
			}
			sb.WriteString("|")
		}
	}
	if currentColor != "" {
		sb.WriteString(colorReset)
	}
	return sb.String()
}

func renderPowerMeterHalfReversed(filled, halfWidth int, minDb float64) string {
	var sb strings.Builder
	currentColor := ""
	for i := halfWidth - 1; i >= 0; i-- {
		posDB := minDb + (float64(i)/float64(halfWidth))*(-minDb)
		if i < halfWidth-filled {
			if currentColor != "" {
				sb.WriteString(colorReset)
				currentColor = ""
			}
			sb.WriteString("·")
		} else {
			c := colorForDb(posDB)
			if c != currentColor {
				sb.WriteString(c)
				currentColor = c
			}
			sb.WriteString("|")
		}
	}
	if currentColor != "" {
		sb.WriteString(colorReset)
	}
	return sb.String()
}

func (ap *AudioPlayer) renderPowerMeter() {
	x := ap.audioRMS.Load()
	bl := uint32(x >> 32)
	br := uint32(x)
	rmsL := math.Float32frombits(bl)
	rmsR := math.Float32frombits(br)

	// Convert power to decibels
	const minDB = -40.0
	dbL := max(minDB, 20*math.Log10(float64(rmsL)/32768.0))
	dbR := max(minDB, 20*math.Log10(float64(rmsR)/32768.0))

	// Use terminal width for power meter, accounting for brackets
	width := ap.termWidth - 4 // Reserve 2 chars for brackets on each half
	halfWidth := width / 2

	filledL := int((dbL - minDB) / (-minDB) * float64(halfWidth))
	filledR := int((dbR - minDB) / (-minDB) * float64(halfWidth))
	filledL = max(0, min(filledL, halfWidth))
	filledR = max(0, min(filledR, halfWidth))

	leftBar := renderPowerMeterHalf(filledL, halfWidth, minDB)
	rightBar := renderPowerMeterHalfReversed(filledR, halfWidth, minDB)

	fmt.Fprintf(ap.uiWriter, "[%s][%s]\n", leftBar, rightBar)
}

// renderInstrumentStatus shows which instruments are playing on each channel
func (ap *AudioPlayer) renderInstrumentStatus(state modplayer.PlayerState) {
	song := ap.player.Song

	// Process channels in pairs for centering
	for i := 0; i < len(state.Channels); i += 2 {
		var lineText strings.Builder

		// First channel in the pair
		ch := state.Channels[i]
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
		lineText.WriteString(fmt.Sprintf("%-32s", outs))

		// Second channel in the pair (if it exists)
		if i+1 < len(state.Channels) {
			ch2 := state.Channels[i+1]
			tc2 := ' '
			if state.Order == ch2.TrigOrder && state.Row == ch2.TrigRow {
				tc2 = '■'
			} else if ch2.Instrument != -1 {
				tc2 = '□'
			}
			outs2 := fmt.Sprintf("%2d%c ", i+2, tc2)
			si2 := ch2.Instrument
			if si2 != -1 {
				outs2 += song.Samples[si2].Name
			}
			lineText.WriteString(fmt.Sprintf("%-32s", outs2))
		}

		// Center the line using a fixed width (64 chars for full line, 32 for single channel)
		const fullLineWidth = 64
		padding := max((ap.termWidth-fullLineWidth)/2, 0)
		fmt.Fprintf(ap.uiWriter, "%*s%s\n", padding, "", lineText.String())
	}
	fmt.Fprintln(ap.uiWriter)
	fmt.Fprintln(ap.uiWriter)
}

// renderChannelHeaders renders the channel number headers
func (ap *AudioPlayer) renderChannelHeaders() {
	song := ap.player.Song

	// Calculate content width for centering (including row prefix and suffix)
	const rowPrefixWidth = 4 // "    " or ">>> "
	const rowSuffixWidth = 4 // "    " or " <<<"
	maxChannels := min(song.Channels, ap.layout.maxChannels)
	contentWidth := rowPrefixWidth + maxChannels*ap.layout.channelWidth + rowSuffixWidth
	if maxChannels > 1 {
		contentWidth += (maxChannels - 1) * ap.layout.separatorWidth
	}
	if song.Channels > maxChannels {
		contentWidth += 4 // " ..."
	}

	// Center the headers
	leftPadding := max((ap.termWidth-contentWidth)/2, 0)
	fmt.Fprint(ap.uiWriter, strings.Repeat(" ", leftPadding))
	fmt.Fprint(ap.uiWriter, "    ")

	for i := range maxChannels {
		// Format channel number to fit within channel width
		chanWidth := ap.layout.channelWidth
		format := fmt.Sprintf("%%-%dd", chanWidth)

		if i == ap.selectedChannel {
			fmt.Fprint(ap.uiWriter, green(format, i+1))
		} else {
			fmt.Fprintf(ap.uiWriter, format, i+1)
		}

		// Add separator except after last channel
		if i < maxChannels-1 {
			fmt.Fprint(ap.uiWriter, "|")
		}
	}

	// Show overflow indicator if there are more channels
	if song.Channels > maxChannels {
		fmt.Fprint(ap.uiWriter, " ...")
	}

	// Add suffix spacing to align with data rows
	fmt.Fprint(ap.uiWriter, "    ")

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

	// Calculate content width for centering (including row prefix and suffix)
	// Always include suffix width in calculation so all rows align
	const rowPrefixWidth = 4 // "    " or ">>> "
	const rowSuffixWidth = 4 // " <<<" when current
	maxChannels := min(len(nd), ap.layout.maxChannels)
	contentWidth := rowPrefixWidth + maxChannels*ap.layout.channelWidth + rowSuffixWidth
	if maxChannels > 1 {
		contentWidth += (maxChannels - 1) * ap.layout.separatorWidth
	}
	if len(nd) > maxChannels {
		contentWidth += 4 // " ..."
	}

	// Center the content
	leftPadding := max((ap.termWidth-contentWidth)/2, 0)
	fmt.Fprint(ap.uiWriter, strings.Repeat(" ", leftPadding))

	// Row prefix
	if isCurrent {
		fmt.Fprint(ap.uiWriter, ">>> ")
	} else {
		fmt.Fprint(ap.uiWriter, "    ")
	}

	// Note data for each channel
	for ni, n := range nd {
		if ni >= maxChannels {
			if ni == maxChannels {
				fmt.Fprint(ap.uiWriter, " ...")
			}
			break
		}

		ap.formatter.formatNote(n, ap.uiWriter)

		// Add separator except after last channel
		if ni < maxChannels-1 {
			fmt.Fprint(ap.uiWriter, "|")
		}
	}

	// Row suffix (always print spaces to maintain alignment)
	if isCurrent {
		fmt.Fprint(ap.uiWriter, " <<<")
	} else {
		fmt.Fprint(ap.uiWriter, "    ")
	}
	fmt.Fprintln(ap.uiWriter)
}

// formatNote formats and writes a single note to the writer
func (nf *noteFormatter) formatNote(n modplayer.ChannelNoteData, w io.Writer) {
	switch nf.mode {
	case displayModeWide:
		nf.formatWide(n, w)
	case displayModeNarrow:
		nf.formatNarrow(n, w)
	case displayModeCompact:
		nf.formatCompact(n, w)
	}
}

// formatWide formats a note in wide display mode (shows all details)
func (nf *noteFormatter) formatWide(n modplayer.ChannelNoteData, w io.Writer) {
	note := n.Note
	if note == "   " {
		note = "..."
	}
	fmt.Fprint(w, blue("%s", note), " ", cyan("%2X", n.Instrument), " ")
	if n.Volume != 0xFF {
		fmt.Fprint(w, green("%02X", n.Volume))
	} else {
		fmt.Fprint(w, green(".."))
	}
	fmt.Fprint(w, " ", magenta("%02X", n.Effect), yellow("%02X", n.Param))
}

// formatNarrow formats a note in narrow display mode (omits instrument and volume)
func (nf *noteFormatter) formatNarrow(n modplayer.ChannelNoteData, w io.Writer) {
	note := n.Note
	if note == "   " {
		note = "..."
	}
	fmt.Fprint(w, blue("%s", note), " ", magenta("%02X", n.Effect), yellow("%02X", n.Param))
}

// formatCompact formats a note in compact display mode (note only)
func (nf *noteFormatter) formatCompact(n modplayer.ChannelNoteData, w io.Writer) {
	note := n.Note
	if note == "   " {
		note = "..."
	}
	fmt.Fprint(w, blue("%s", note))
}

// computeChannelLayout determines the optimal display layout based on channel count and terminal width
// Priority: Try to show ALL channels. Use the widest mode that fits all channels.
// If all channels won't fit even in compact mode, show as many as possible.
func computeChannelLayout(channels, termWidth int) channelLayout {
	// Account for row prefix (">>> " or "    "), suffix (" <<<" or "    "), and margin
	const rowPrefixWidth = 4
	const rowSuffixWidth = 4
	const minMargin = 4

	availableWidth := termWidth - rowPrefixWidth - rowSuffixWidth - minMargin

	// Try each display mode from widest to narrowest
	// Goal: fit ALL channels in the widest mode possible
	modes := []struct {
		mode           displayMode
		channelWidth   int
		separatorWidth int
	}{
		{displayModeWide, 14, 1},   // "C-5 01 40 0A00|" = 15 chars (14 + 1 separator)
		{displayModeNarrow, 8, 1},  // "C-5 0A00|" = 9 chars (8 + 1 separator)
		{displayModeCompact, 3, 1}, // "C-5|" = 4 chars (3 + 1 separator)
	}

	for _, m := range modes {
		// Calculate width needed for ALL channels
		widthNeeded := 0
		for i := range channels {
			widthNeeded += m.channelWidth
			if i < channels-1 {
				widthNeeded += m.separatorWidth
			}
		}

		// If all channels fit in this mode, use it
		if widthNeeded <= availableWidth {
			return channelLayout{
				displayMode:    m.mode,
				channelWidth:   m.channelWidth,
				maxChannels:    channels,
				separatorWidth: m.separatorWidth,
			}
		}
	}

	// If even compact mode can't fit all channels, show as many as we can in compact mode
	// Minimum display is just the note (3 chars) - this is our floor
	compactWidth := 3
	separatorWidth := 1

	maxChannels := 0
	widthNeeded := 0
	for i := range channels {
		channelAndSep := compactWidth
		if i < channels-1 {
			channelAndSep += separatorWidth
		}

		if widthNeeded+channelAndSep <= availableWidth {
			maxChannels++
			widthNeeded += channelAndSep
		} else {
			break
		}
	}

	return channelLayout{
		displayMode:    displayModeCompact,
		channelWidth:   compactWidth,
		maxChannels:    maxChannels,
		separatorWidth: separatorWidth,
	}
}

// getChannelWidth returns the width in characters for a channel in the given display mode
func getChannelWidth(mode displayMode) int {
	switch mode {
	case displayModeWide:
		return 14 // "C-5 01 40 0A00" (note, space, inst, space, vol, space, effect+param)
	case displayModeNarrow:
		return 8 // "C-5 0A00" (note, space, effect+param)
	case displayModeCompact:
		return 3 // "C-5" (note only)
	default:
		return 8
	}
}

// getMaxChannelsForWidth computes how many channels can fit in the available width
func getMaxChannelsForWidth(mode displayMode, availableWidth int) int {
	channelWidth := getChannelWidth(mode)
	separatorWidth := 1 // "|"

	if availableWidth <= 0 {
		return 0
	}

	// First channel doesn't need leading separator
	maxChannels := 1
	usedWidth := channelWidth

	// Try to fit more channels
	for {
		nextChannelWidth := separatorWidth + channelWidth
		if usedWidth+nextChannelWidth <= availableWidth {
			maxChannels++
			usedWidth += nextChannelWidth
		} else {
			break
		}
	}

	return maxChannels
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
