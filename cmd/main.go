// MOD player in Go
// Useful notes https://github.com/AntonioND/gbt-player/blob/master/mod2gbt/FMODDOC.TXT
// Uses portaudio for audio output

package main

import (
	"flag"
	"io/ioutil"
	"log"
	"os"

	mod "github.com/chriskillpack/mod_player"
	"github.com/chriskillpack/mod_player/wav"
	"github.com/gordonklaus/portaudio"
)

const (
	outputBufferHz = 44100
)

// TODO
// 1) Figure out how to disable portaudio debug text

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Missing MOD filename")
	}

	wavOut := flag.String("wav", "", "output to a WAVE file")
	flag.Parse()

	modF, err := ioutil.ReadFile(flag.Args()[0])
	if err != nil {
		log.Fatal(err)
	}

	hdr, err := mod.NewSongFromBytes(modF)
	if err != nil {
		log.Fatal(err)
	}

	player := mod.NewPlayer(hdr, outputBufferHz)

	if *wavOut == "" {
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

		<-player.EndCh // wait for song to end
	} else {
		wavF, err := os.Create(*wavOut)
		if err != nil {
			log.Fatal(err)
		}
		defer wavF.Close()

		var wavW *wav.Writer
		if wavW, err = wav.NewWriter(wavF, outputBufferHz); err != nil {
			log.Fatal(err)
		}

		audioOut := make([]int16, 2048)

		playing := true
		go func() {
			for playing {
				pl := true

				select {
				case _ = <-player.EndCh:
					pl = false
				default:
				}

				player.GenerateAudio(audioOut)
				if err = wavW.WriteFrame(audioOut); err != nil {
					wavF.Close()
					log.Fatal(err)
				}
				playing = pl
			}
		}()

		// TODO: yuck! do something better
		for playing {
		}

		wavW.Finish()
		wavF.Close()
	}
}
