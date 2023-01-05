package main

import (
	"flag"
	"log"
	"os"

	"github.com/chriskillpack/modplayer"
	"github.com/gordonklaus/portaudio"
)

var (
	flagHz       = flag.Int("hz", 44100, "output hz")
	flagBoost    = flag.Int("boost", 1, "volume boost, an integer between 1 and 4")
	flagStartOrd = flag.Int("start", 0, "starting order in the MOD, clamped to song max")
)

const (
	escape     = "\x1b["
	hideCursor = escape + "?25l"
	showCursor = escape + "?25h"
)

// Comb models a simple Comb filter reverb module. At construction time it takes
// a block of sample data and applies reverb to it. It cannot be fed any more
// sample data after this.
type Comb struct {
	delayOffset int
	readPos     int
	audio       []int16
}

func newComb(in []int16, decay float32, delayMs, sampleRate int) *Comb {
	c := &Comb{
		delayOffset: (delayMs * sampleRate) / 1000,
		audio:       make([]int16, len(in)),
	}

	copy(c.audio, in)
	for i := 0; i < len(in)/2-c.delayOffset; i++ {
		c.audio[(i+c.delayOffset)*2+0] += int16(float32(c.audio[i*2+0]) * decay)
		c.audio[(i+c.delayOffset)*2+1] += int16(float32(c.audio[i*2+1]) * decay)
	}

	return c
}

func (c *Comb) GetAudio(out []int16) int {
	n := len(out)
	if c.readPos+n > len(c.audio) {
		n = len(c.audio) - c.readPos
	}
	copy(out, c.audio[c.readPos:c.readPos+n])
	c.readPos += n
	return n
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("modplay: ")
	flag.Parse()

	if len(flag.Args()) == 0 {
		log.Fatal("Missing MOD filename")
	}

	modF, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
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

	initErr := portaudio.Initialize()
	defer func() {
		if initErr != nil {
			portaudio.Terminate()
		}
	}()

	// Generate a large buffer of audio
	ns := 1000 * 1024
	audio := make([]int16, ns*2)
	player.GenerateAudio(audio)
	c := newComb(audio, 0.2, 75, *flagHz)

	var stream *portaudio.Stream
	streamCB := func(out []int16) {
		x := make([]int16, len(out))
		player.GenerateAudio(x)
		n := c.GetAudio(out)
		if n == 0 {
			player.Stop()
		}
	}

	stream, err = portaudio.OpenDefaultStream(0, 2, float64(*flagHz), portaudio.FramesPerBufferUnspecified, streamCB)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	stream.Start()
	defer stream.Stop()

	for player.IsPlaying() {
	}
}
