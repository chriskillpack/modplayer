// MOD player in Go
// Uses portaudio for audio output or can write to WAV file (16-bit, stereo)

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/cmd/modwav/wav"
	"github.com/chriskillpack/modplayer/internal/comb"
)

var (
	flagWAVOut   = flag.String("wav", "", "output location for WAV file")
	flagHz       = flag.Int("hz", 44100, "output hz")
	flagBoost    = flag.Int("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Int("start", 0, "starting order in the MOD, clamped to song max")
	flagNoReverb = flag.Bool("noreverb", false, "disable reverb")
)

func main() {
	// cf := comb.NewCombFixed(0, 0.1, 10, *flagHz)
	// bloop := make([]int16, 2048)
	// cf.InputSamples(bloop[:30])
	// cf.InputSamples(bloop[:500])
	// cf.GetAudio(bloop[:530])
	// cf.GetAudio(bloop[:30])
	// cf.InputSamples(bloop[:700])
	// cf.GetAudio(bloop[:1000])

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

	var scratch []int16
	var c *comb.CombAdd
	if !*flagNoReverb {
		c = comb.NewCombAdd(100*1024, 0.2, 150, *flagHz)
		scratch = make([]int16, 10*1024)
	}
	audioOut := make([]int16, 2048)
	cf2 := comb.NewCombFixed(1024, 0.2, 3500, *flagHz)

	for player.IsPlaying() {
		var n int
		if !*flagNoReverb {
			sc := scratch[:len(audioOut)]
			n = player.GenerateAudio(sc) * 2
			c.InputSamples(sc[:n])
			cf2.InputSamples(sc[:n])
			n = cf2.GetAudio(audioOut)
			// n = c.GetAudio(audioOut)
		} else {
			n = player.GenerateAudio(audioOut) * 2
		}
		if err = wavW.WriteFrame(audioOut[:n]); err != nil {
			wavF.Close()
			log.Fatal(err)
		}
	}

	player.Stop()
	fmt.Printf("Remaining %d\n", cf2.N())
}
