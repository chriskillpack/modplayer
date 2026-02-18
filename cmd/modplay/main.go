package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/cmd/internal/config"
)

var (
	flagHz       = flag.Int("hz", 44100, "output hz")
	flagBoost    = flag.Int("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Int("start", 0, "starting order in the MOD, clamped to song max")
	flagLenOrd   = flag.Int("maxpatterns", -1, "Maximum number of orders to play, useful for songs that loop forever")
	flagReverb   = flag.String("reverb", "light", "choose from light, medium, hall or none")
	flagMute     = flag.Uint("mute", 0, "bitmask of muted channels, channel 1 in LSB, set bit to mute channel")
	flagNoUI     = flag.Bool("noui", false, "turn off all UI, mostly useful in development")
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("modplay: ")
	flag.Parse()

	if len(flag.Args()) == 0 {
		log.Fatal("Missing song filename")
	}

	songFName := flag.Arg(0)
	songF, err := os.ReadFile(songFName)
	if err != nil {
		log.Fatal(err)
	}

	var song *modplayer.Song
	switch strings.ToLower(filepath.Ext(songFName)) {
	case ".mod":
		song, err = modplayer.NewMODSongFromBytes(songF)
	case ".s3m":
		song, err = modplayer.NewS3MSongFromBytes(songF)
	default:
		err = fmt.Errorf("unsupported song %q", songFName)
	}
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
	player.Mute = *flagMute
	if *flagStartOrd > 0 {
		player.SeekTo(*flagStartOrd, 0)
	}
	player.PlayOrderLimit = *flagLenOrd

	rvb, err := config.ReverbFromFlag(*flagReverb, *flagHz)
	if err != nil {
		log.Fatal(err)
	}

	play(player, rvb)
}
