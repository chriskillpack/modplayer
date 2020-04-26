package main

import (
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

	stream, err := portaudio.OpenDefaultStream(0, 2, float64(outputBufferHz), portaudio.FramesPerBufferUnspecified, player.GenerateAudio)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	stream.Start()
	defer stream.Stop()

	<-player.EndCh // wait for song to end}
}
