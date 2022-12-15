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

	var lastPos modplayer.PlayerPosition
	for player.IsPlaying() {
		pos := player.Position()

		if lastPos.Notes != nil && lastPos.Order == pos.Order && lastPos.Row == pos.Row {
			continue
		}

		if len(song.Title) > 0 {
			fmt.Print(song.Title + " ")
		}
		fmt.Printf("%s %02X/3F %s %02X/%02X %s %d %s %d\n\n", blue("row"), pos.Row, blue("pat"), pos.Order, len(song.Orders), blue("speed"), player.Speed, blue("bpm"), player.Tempo)

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
		fmt.Print(escape + "11F") // move cursor to beginning of line 9 above
	}

	// Show the cursor
	fmt.Print(showCursor)
}
