package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/chriskillpack/modplayer"
	"github.com/chriskillpack/modplayer/cmd/internal/config"
	"github.com/gordonklaus/portaudio"
)

var (
	flagHz       = flag.Int("hz", 44100, "output hz")
	flagBoost    = flag.Int("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Int("start", 0, "starting order in the MOD, clamped to song max")
	flagLenOrd   = flag.Int("maxpatterns", -1, "Maximum number of orders to play, useful for songs that loop forever")
	flagReverb   = flag.String("reverb", "light", "choose from light, medium, silly or none")
	flagMute     = flag.Uint("mute", 0, "bitmask of muted channels, channel 1 in LSB, set bit to mute channel")
)

const (
	escape     = "\x1b["
	hideCursor = escape + "?25l"
	showCursor = escape + "?25h"
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

	initErr := portaudio.Initialize()
	defer func() {
		if initErr != nil {
			portaudio.Terminate()
		}
	}()

	doddi, _ := portaudio.DefaultOutputDevice()
	fmt.Printf("dod: %+v\n", doddi)

	rvb, err := config.ReverbFromFlag(*flagReverb, *flagHz)
	if err != nil {
		log.Fatal(err)
	}

	// var ticker int

	scratch := make([]int16, 10*1024)
	streamCB := func(out []int16) {
		sc := scratch[:len(out)]
		player.GenerateAudio(sc)
		rvb.InputSamples(sc)
		n := rvb.GetAudio(out)

		if n == 0 || player.State().Row >= 6 {
			player.Stop()
		}
	}

	stream, err := portaudio.OpenDefaultStream(0, 2, float64(*flagHz), int(portaudio.FramesPerBufferUnspecified), streamCB)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	fmt.Printf("stream: %v\n", stream.Info())

	stream.Start()
	defer stream.Stop()

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, syscall.SIGINT)
	go func() {
		<-sigch
		player.Stop()
		stream.Stop()
		portaudio.Terminate()

		os.Exit(0)
	}()

	// Print out some player preceeding 4 rows, current row and upcoming 4 rows
	// <title> row 1A/3F pat 0A/73 speed 6 bpm 125
	//
	//          0 0000|     0 0C00|^^.  0 0000|     0 0000
	//          0 0000|     0 0000|     0 0000|     0 0000
	//     C#5  F 0000|G-5 14 0000|     0 0000|     0 0000
	//          0 0000|     0 0000|     0 0000|     0 0000
	// >>>      0 0000|     0 0000|     0 0000|     0 0000 <<<
	//          0 0000|     0 0000|     0 0000|     0 0000
	//          0 0000|G-5 14 0C0B|     0 0000|     0 0000
	//          0 0000|     0 0000|     0 0000|     0 0000
	//     C#5  F 0000|     0 0000|     0 0000|     0 0000

	var lastState modplayer.PlayerState
	for player.IsPlaying() {
		state := player.State()

		if lastState.Notes != nil && lastState.Order == state.Order && lastState.Row == state.Row {
			continue
		}

	}
}
