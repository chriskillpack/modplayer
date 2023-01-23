package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chriskillpack/modplayer"
	"github.com/fatih/color"
	"github.com/gordonklaus/portaudio"
)

var (
	flagHz       = flag.Int("hz", 44100, "output hz")
	flagBoost    = flag.Int("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Int("start", 0, "starting order in the MOD, clamped to song max")
)

const (
	escape     = "\x1b["
	hideCursor = escape + "?25l"
	showCursor = escape + "?25h"
)

// Comb models a simple Comb filter reverb module. At construction time it takes
// a block of sample data and applies reverb to it. It cannot be fed any more
// sample data after this.
type Comb struct {
	delayOffset int
	readPos     int
	audio       []int16
}

func newComb(in []int16, decay float32, delayMs, sampleRate int) *Comb {
	c := &Comb{
		delayOffset: (delayMs * sampleRate) / 1000,
		audio:       make([]int16, len(in)),
	}

	copy(c.audio, in)
	for i := 0; i < len(in)/2-c.delayOffset; i++ {
		c.audio[(i+c.delayOffset)*2+0] += int16(float32(c.audio[i*2+0]) * decay)
		c.audio[(i+c.delayOffset)*2+1] += int16(float32(c.audio[i*2+1]) * decay)
	}

	return c
}

func (c *Comb) GetAudio(out []int16) int {
	n := len(out)
	if c.readPos+n > len(c.audio) {
		n = len(c.audio) - c.readPos
	}
	copy(out, c.audio[c.readPos:c.readPos+n])
	c.readPos += n
	return n
}

// CombAdd is a Comb filter can be fed audio data incrementally
// It does not discard used samples and has no upper bound on memory used
type CombAdd struct {
	Comb
	readPos  int
	writePos int
	decay    float32
}

// initialSize is in sample pairs
func newCombAdd(initialSize int, decay float32, delayMs, sampleRate int) *CombAdd {
	c := &CombAdd{
		Comb: Comb{
			delayOffset: (delayMs * sampleRate) / 1000,
			audio:       make([]int16, 0, initialSize*2),
		},
		decay: decay,
	}

	return c
}

// InputSamples feeds the CombAdd filter with new sample data. Once enough
// samples have been accumulated the filter will start applying reverb to audio
// data. The exact number of samples is determined by delay and sample rate.
// InputSamples returns the number of samples required before reverb can be
// applied. The functions takes a copy of the provided audio data.
func (c *CombAdd) InputSamples(in []int16) int {
	c.audio = append(c.audio, in...)
	if len(c.audio) > c.delayOffset*2 {
		ns := len(c.audio) - (c.delayOffset*2 + c.writePos)
		for i := 0; i < ns; i++ {
			c.audio[i+c.delayOffset*2+c.writePos] += int16(float32(c.audio[i+c.writePos]) * c.decay)
		}
		c.writePos += ns
	}
	rem := c.delayOffset*2 - len(c.audio)
	if rem < 0 {
		rem = 0
	}
	return rem
}

// GetAudio puts processed audio data into the out slice. It returns the number
// of samples put into out.
func (c *CombAdd) GetAudio(out []int16) int {
	wanted := len(out)
	have := len(c.audio) - c.readPos
	if wanted > have {
		wanted = have
	}
	if wanted > 0 {
		copy(out, c.audio[c.readPos:c.readPos+wanted])
		c.readPos += wanted
	}
	return wanted
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("modplay: ")
	flag.Parse()

	if len(flag.Args()) == 0 {
		log.Fatal("Missing MOD filename")
	}

	modF, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	song, err := modplayer.NewSongFromBytes(modF)
	if err != nil {
		log.Fatal(err)
	}

	player, err := modplayer.NewPlayer(song, uint(*flagHz))
	if err != nil {
		log.Fatal(err)
	}
	if err := player.SetVolumeBoost(*flagBoost); err != nil {
		log.Fatal(err)
	}
	player.SeekTo(*flagStartOrd, 0)

	initErr := portaudio.Initialize()
	defer func() {
		if initErr != nil {
			portaudio.Terminate()
		}
	}()

	c := newCombAdd(100*1024, 0.2, 150, *flagHz)
	scratch := make([]int16, 10*1024)

	var stream *portaudio.Stream
	streamCB := func(out []int16) {
		sc := scratch[:len(out)]
		player.GenerateAudio(sc)
		c.InputSamples(sc)
		n := c.GetAudio(out)
		if n == 0 {
			player.Stop()
		}
	}

	stream, err = portaudio.OpenDefaultStream(0, 2, float64(*flagHz), portaudio.FramesPerBufferUnspecified, streamCB)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	stream.Start()
	defer stream.Stop()

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, syscall.SIGINT)
	go func() {
		<-sigch
		player.Stop()
		stream.Stop()
		portaudio.Terminate()

		fmt.Print(showCursor)
		os.Exit(0)
	}()

	// Hide the cursor
	fmt.Print(hideCursor)

	white := color.New(color.FgWhite).SprintFunc()
	cyan := color.New(color.FgCyan).SprintfFunc()
	magenta := color.New(color.FgMagenta).SprintfFunc()
	yellow := color.New(color.FgYellow).SprintfFunc()
	blue := color.New(color.FgHiBlue).SprintFunc()

	// Print out some player preceeding 4 rows, current row and upcoming 4 rows
	// <title> row 1A/3F pat 0A/73 speed 6 bpm 125
	//
	//          0 000|     0 C00|     0 000|     0 000
	//          0 000|     0 000|     0 000|     0 000
	//     C#5  F 000|G-5 14 000|     0 000|     0 000
	//          0 000|     0 000|     0 000|     0 000
	// >>>      0 000|     0 000|     0 000|     0 000 <<<
	//          0 000|     0 000|     0 000|     0 000
	//          0 000|G-5 14 C0B|     0 000|     0 000
	//          0 000|     0 000|     0 000|     0 000
	//     C#5  F 000|     0 000|     0 000|     0 000

	var lastState modplayer.PlayerState
	for player.IsPlaying() {
		state := player.State()

		if lastState.Notes != nil && lastState.Order == state.Order && lastState.Row == state.Row {
			continue
		}

		if len(song.Title) > 0 {
			fmt.Print(song.Title + " ")
		}
		fmt.Printf("%s %02X/3F %s %02X/%02X %s %d %s %d\n", blue("row"), state.Row, blue("pat"), state.Order, len(song.Orders), blue("speed"), player.Speed, blue("bpm"), player.Tempo)

		// Print out some channel info
		ncl := len(state.Channels) / 2
		for i, ch := range state.Channels {
			outs := fmt.Sprintf("%2d: ", i+1)

			si := ch.Instrument
			if si != -1 {
				outs += song.Samples[si].Name
			}
			if len(outs) < 26 {
				outs = fmt.Sprintf("%-26s", outs)
			}
			fmt.Print(outs)
			if i&1 == 1 {
				fmt.Println()
			}
		}
		fmt.Println()

		for i := -4; i <= 4; i++ {
			nd := player.NoteDataFor(state.Order, state.Row+i)
			if nd == nil {
				fmt.Println()
				continue
			}

			// If this is the currently playing row then highlight it
			if i == 0 {
				fmt.Print(">>> ")
			} else {
				fmt.Print("    ")
			}

			// Print out the first 4 channels of note data
			for ni, n := range nd {
				if ni < 4 {
					fmt.Print(white(n.Note), " ", cyan("%2X", n.Instrument), " ", magenta("%X", n.Effect), yellow("%02X", n.Param))
					if ni < 3 {
						fmt.Print("|")
					}
				} else if ni == 4 {
					fmt.Print(" ...")
					break
				}
			}
			if i == 0 {
				fmt.Print(" <<<")
			}
			fmt.Println()
		}
		fmt.Printf(escape+"%dF", 11+ncl) // move cursor to beginning of line 9 above
	}

	// Show the cursor
	fmt.Print(showCursor)
}
