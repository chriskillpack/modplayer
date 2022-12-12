package main

import (
	"flag"
	"fmt"
	"io/ioutil"
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
	flagBoost    = flag.Uint("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Int("start", 0, "starting order in the MOD, clamped to song max")
)

const (
	escape     = "\x1b["
	hideCursor = escape + "?25l"
	showCursor = escape + "?25h"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("modplay: ")
	flag.Parse()

	if len(flag.Args()) == 0 {
		log.Fatal("Missing MOD filename")
	}

	modF, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	song, err := modplayer.NewSongFromBytes(modF)
	if err != nil {
		log.Fatal(err)
	}

	player, err := modplayer.NewPlayer(song, uint(*flagHz), *flagBoost)
	if err != nil {
		log.Fatal(err)
	}
	player.SeekTo(*flagStartOrd, 0)

	initErr := portaudio.Initialize()
	defer func() {
		if initErr != nil {
			portaudio.Terminate()
		}
	}()

	streamCB := func(out []int16) {
		player.GenerateAudio(out)
	}

	stream, err := portaudio.OpenDefaultStream(0, 2, float64(*flagHz), portaudio.FramesPerBufferUnspecified, streamCB)
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

	fmt.Println(song.Title)

	white := color.New(color.FgWhite).SprintFunc()
	cyan := color.New(color.FgCyan).SprintfFunc()
	magenta := color.New(color.FgMagenta).SprintfFunc()
	yellow := color.New(color.FgYellow).SprintfFunc()

	// Print out preceeding 4 lines, current line and upcoming 4 lines
	var lastPos modplayer.PlayerPosition
	for player.IsPlaying() {
		pos := player.Position()

		if lastPos.Notes != nil && lastPos.Order == pos.Order && lastPos.Row == pos.Row {
			continue
		}

		for i := -4; i <= 4; i++ {
			nd := player.NoteDataFor(pos.Order, pos.Row+i)
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

			// Print out the first 4 channels of
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
		fmt.Print(escape + "9F") // move cursor to beginning of line 9 above
	}

	// Show the cursor
	fmt.Print(showCursor)
}
