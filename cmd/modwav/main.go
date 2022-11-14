// MOD player in Go
// Uses portaudio for audio output or can write to WAV file (16-bit, stereo)

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/cmd/modwav/wav"
)

var (
	flagWAVOut = flag.String("wav", "", "output location for WAV file")
	flagHz     = flag.Int("hz", 44100, "output hz")
	flagBoost  = flag.Uint("boost", 1, "volume boost, an integer between 1 and 4")
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("modwav: ")

	if len(os.Args) < 2 {
		log.Fatal("Missing MOD filename")
	}

	flag.Parse()
	modName := flag.Args()[0]
	modF, err := ioutil.ReadFile(modName)
	if err != nil {
		log.Fatal(err)
	}

	// If no output file was specified then default to the current directory
	// with the same filename and a '.wav' extension, e.g.
	// /music/songs/mod/foo.mod would default to ./foo.wav
	if *flagWAVOut == "" {
		// If no WAV file output specified, write it out the current directory
		base := filepath.Base(modName)
		baseStripped := base[:len(base)-len(filepath.Ext(modName))]
		*flagWAVOut = baseStripped + ".wav"
	}

	song, err := modplayer.NewSongFromBytes(modF)
	if err != nil {
		log.Fatal(err)
	}

	player, err := modplayer.NewPlayer(song, uint(*flagHz), *flagBoost)
	if err != nil {
		log.Fatal(err)
	}

	wavF, err := os.Create(*flagWAVOut)
	if err != nil {
		log.Fatal(err)
	}
	defer wavF.Close()

	var wavW *wav.Writer
	if wavW, err = wav.NewWriter(wavF, *flagHz); err != nil {
		log.Fatal(err)
	}
	defer wavW.Finish()

	audioOut := make([]int16, 2048)

	var lastPos modplayer.PlayerPosition
	for player.IsPlaying() {
		pos := player.Position()
		if lastPos.Notes == nil || (lastPos.Order != pos.Order) {
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
