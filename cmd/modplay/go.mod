module github.com/chriskillpack/modplayer/cmd/modplay

go 1.25

require (
	atomicgo.dev/keyboard v0.2.9
	github.com/chriskillpack/modplayer v0.1.0
	github.com/fatih/color v1.13.0
	github.com/gordonklaus/portaudio v0.0.0-20230709114228-aafa478834f5
)

require (
	github.com/containerd/console v1.0.3 // indirect
	github.com/mattn/go-colorable v0.1.9 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/term v0.39.0 // indirect
)

replace github.com/chriskillpack/modplayer v0.1.0 => ../../
