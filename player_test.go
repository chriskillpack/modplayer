package modplayer

import (
	"bytes"
	"os"
	"testing"
)

func TestLoadSong(t *testing.T) {
	mod, err := os.ReadFile("mods/space_debris.mod")
	if err != nil {
		t.Fatal(err)
	}
	song, err := NewSongFromBytes(mod)
	if err != nil {
		t.Fatal(err)
	}
	if song.Title != "space_debris" {
		t.Errorf("Incorrect song title %s", song.Title)
	}
	if song.Channels != 4 {
		t.Errorf("Expecting 4 channels, got %d", song.Channels)
	}
	if len(song.Orders) != 42 {
		t.Errorf("Expecting 42 orders, got %d", len(song.Orders))
	}
	if !bytes.Equal(song.Orders[0:3], []byte{1, 2, 3}) || song.Orders[41] != 0x28 {
		t.Errorf("Order data is wrong")
	}
}

func BenchmarkMixChannels(b *testing.B) {
	mod, err := os.ReadFile("testdata/mix.mod")
	if err != nil {
		b.Fatal(err)
	}
	song, err := NewSongFromBytes(mod)
	if err != nil {
		b.Fatal(err)
	}
	player, err := NewPlayer(song, 44100, 1)
	if err != nil {
		b.Fatal(err)
	}

	out := make([]int16, 1024*2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		player.GenerateAudio(out) // internally this calls MixChannels
	}
}
