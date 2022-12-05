package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/chriskillpack/modplayer"
	"github.com/gordonklaus/portaudio"
)

var (
	flagHz       = flag.Int("hz", 44100, "output hz")
	flagBoost    = flag.Uint("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Uint("start", 0, "starting order in the MOD, clamped to song max")
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
	player.SetOrder(*flagStartOrd)
	if err != nil {
		log.Fatal(err)
	}

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

	lastPos := modplayer.PlayerPosition{Order: -1}
	for player.IsPlaying() {
		pos := player.Position()
		if lastPos.Order != pos.Order || lastPos.Row != pos.Row {
			fmt.Printf("%02X %02X|", pos.Order, pos.Row)
			for i, n := range pos.Notes {
				if i < 4 {
					fmt.Print(n.String())
					if i < 3 {
						fmt.Print("|")
					}
				} else if i == 4 {
					fmt.Print(" ...")
				}
			}
			fmt.Println()
			lastPos = pos
		}
	}
}
