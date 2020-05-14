// MOD player in Go
// Uses portaudio for audio output or can write to WAV file (16-bit, stereo)

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/cmd/modwav/wav"
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

	audioOut := make([]int16, 2048)

	// TODO: Make the zero object useful so we don't have to initialize with
	// -1 to get position updates working correctly.
	lastPos := modplayer.PlayerPosition{Order: -1}
	for player.IsPlaying() {
		pos := player.Position()
		if lastPos.Order != pos.Order {
			fmt.Printf("%d/%d\n", pos.Order+1, len(player.Song.Orders))
			lastPos = pos
		}

		generated := player.GenerateAudio(audioOut)
		if err = wavW.WriteFrame(audioOut[:generated*2]); err != nil {
			wavF.Close()
			log.Fatal(err)
		}
	}
	player.Stop()
}
