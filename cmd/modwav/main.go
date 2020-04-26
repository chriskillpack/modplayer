// MOD player in Go
// Uses portaudio for audio output or can write to WAV file (16-bit, stereo)

package main

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/wav"
)

const outputHz = 44100

func main() {
	log.SetFlags(0)
	log.SetPrefix("modwav: ")

	if len(os.Args) < 2 {
		log.Fatal("Missing MOD filename")
	}

	wavOut := flag.String("wav", "", "output to a WAVE file")
	flag.Parse()
	if *wavOut == "" {
		log.Fatal("Not -wav option provided")
	}

	modF, err := ioutil.ReadFile(flag.Args()[0])
	if err != nil {
		log.Fatal(err)
	}

	song, err := modplayer.NewSongFromBytes(modF)
	if err != nil {
		log.Fatal(err)
	}

	player := modplayer.NewPlayer(song, outputHz)

	wavF, err := os.Create(*wavOut)
	if err != nil {
		log.Fatal(err)
	}
	defer wavF.Close()

	var wavW *wav.Writer
	if wavW, err = wav.NewWriter(wavF, outputHz); err != nil {
		log.Fatal(err)
	}
	defer wavW.Finish()

	// Listen for SIGINT to allow a clean exit
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT)

	audioOut := make([]int16, 2048)

	playing := true
	go func() {
		for playing {
			pl := true

			select {
			case <-player.EndCh:
				pl = false
			case <-c:
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
}
