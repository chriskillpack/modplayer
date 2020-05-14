package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/chriskillpack/modplayer"
	"github.com/gordonklaus/portaudio"
)

const outputBufferHz = 44100

// TODO
// 1) Figure out how to disable portaudio debug text

func main() {
	log.SetFlags(0)
	log.SetPrefix("modplay: ")

	if len(os.Args) < 2 {
		log.Fatal("Missing MOD filename")
	}

	modF, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	song, err := modplayer.NewSongFromBytes(modF)
	if err != nil {
		log.Fatal(err)
	}

	player := modplayer.NewPlayer(song, outputBufferHz)

	initErr := portaudio.Initialize()
	defer func() {
		if initErr != nil {
			portaudio.Terminate()
		}
	}()

	streamCB := func(out []int16) {
		player.GenerateAudio(out)
	}

	stream, err := portaudio.OpenDefaultStream(0, 2, float64(outputBufferHz), portaudio.FramesPerBufferUnspecified, streamCB)
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
