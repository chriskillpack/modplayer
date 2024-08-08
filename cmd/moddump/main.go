package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/chriskillpack/modplayer"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("moddump: ")

	if len(os.Args) <= 1 {
		log.Fatal("Missing song filename")
	}

	songFName := os.Args[1]
	songF, err := os.ReadFile(songFName)
	if err != nil {
		log.Fatal(err)
	}

	modplayer.SetDumpWriter(os.Stdout)

	switch strings.ToLower(filepath.Ext(songFName)) {
	case ".mod":
		_, err = modplayer.NewMODSongFromBytes(songF)
	case ".s3m":
		_, err = modplayer.NewS3MSongFromBytes(songF)
	default:
		err = fmt.Errorf("unsupported song %q", songFName)
	}
	if err != nil {
		log.Fatal(err)
	}
}
