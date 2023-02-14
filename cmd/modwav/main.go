// MOD player in Go
// Uses portaudio for audio output or can write to WAV file (16-bit, stereo)

package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/cmd/internal/config"
	"github.com/chriskillpack/modplayer/cmd/modwav/wav"
)

var (
	flagWAVOut   = flag.String("wav", "", "output location for WAV file")
	flagHz       = flag.Int("hz", 44100, "output hz")
	flagBoost    = flag.Int("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Int("start", 0, "starting order in the MOD, clamped to song max")
	flagReverb   = flag.String("reverb", "light", "choose from light, medium, silly or none")
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("modwav: ")
	flag.Parse()

	if len(flag.Args()) == 0 {
		log.Fatal("Missing MOD filename")
	}

	modName := flag.Arg(0)
	modF, err := os.ReadFile(modName)
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

	player, err := modplayer.NewPlayer(song, uint(*flagHz))
	if err != nil {
		log.Fatal(err)
	}
	if err := player.SetVolumeBoost(*flagBoost); err != nil {
		log.Fatal(err)
	}

	player.SeekTo(*flagStartOrd, 0)

	wavF, err := os.Create(*flagWAVOut)
	if err != nil {
		log.Fatal(err)
	}
	defer wavF.Close()

	wavW, err := wav.NewWriter(wavF, *flagHz)
	if err != nil {
		log.Fatal(err)
	}
	defer wavW.Finish()

	rvb, err := config.ReverbFromFlag(*flagReverb, *flagHz)
	if err != nil {
		log.Fatal(err)
	}

	scratch := make([]int16, 2048)
	audioOut := make([]int16, 2048)

	for player.IsPlaying() {
		n := player.GenerateAudio(scratch) * 2
		rvb.InputSamples(scratch[:n])
		n = rvb.GetAudio(audioOut)
		if err = wavW.WriteFrame(audioOut[:n]); err != nil {
			wavF.Close()
			log.Fatal(err)
		}
	}

	player.Stop()
}
