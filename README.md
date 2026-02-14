# MOD and S3M player

_Work in progress_

A Go package to play [MOD](<https://en.wikipedia.org/wiki/MOD_(file_format)>) and [S3M](https://en.wikipedia.org/wiki/S3M) files.

Back in the mid-90's I was active in the PC demoscene and always relied on other people's player code for the music. I never knew how they worked, so I decided many many years later to sit down and code one.

# Usage

The package consists of two main parts, a `Song` and a `Player`. The `Song` struct represents a parsed MOD or S3M file, use `NewMODSongFromBytes` or `NewS3MSongFromBytes` to parse a byte slice holding a music file into a `Song`. Use the correct function for the file type. With a `Song` you can create a `Player` instance. Then call `GenerateAudio` on the `Player` instance to generate raw audio output which you send to an audio device (the `modplay` command) or serialize to disk (the `modwav` command).

# Binaries

There are three binaries provided, `modwav`, `modplay` and `moddump`.

### `modwav`

Generates audio output from MOD and S3M song files, and saves the output in RIFF WAVE format. This is pure Go code with no third party dependencies.

```bash
$ go run ./cmd/modwav -hz 22050 awesome.mod  # Generate a 22.5Khz WAVE file from awesome.mod called awesome.wav
```

You can use the `-hz` and `-wav` command line options to affect quality (default 44.1Khz) and output file, by default the same filename with a `.wav` extension in the current directory. The `-boost` flag can be used to boost the output volume, but this can cause clipping.

### `modplay`

Plays MOD and S3M files through your computers audio out. Go/CGo and uses PortAudio to play the audio. I've included the Windows DLL `portaudio_x64.dll`, you will need to compile portaudio for other platforms. Good luck with that, it can be a bit of a hassle.

```bash
$ export PKG_CONFIG_PATH=$PORTAUDIO
$ export CGO_CFLAGS="-I $PORTAUDIO/include"
$ export CGO_LDFLAGS="-L $PORTAUDIO/lib/.libs"
$ go run ./cmd/modplay awesome.mod
```

![Screenshot of modplay](/docs/modplay.png)

Keyboard

| Key | Description |
|-----|-------------|
| Ctrl-C, Esc | Quit the song |
| Space | Pause/Unpause the song |
| Left Arrow, Right Arrow | Move the selected channel |
| q | Mute/unmute the selected channel |
| s | Solo the selected channel (all other channels are muted). Press again to unmute all channels. |

### `moddump`

Prints the interpreted and raw contents of MOD and S3M files to stdout. The output includes the pattern data and instrument definitions. A really useful tool when debugging.

```bash
$ go run ./cmd/moddump mods/caero.s3m
Name:		
Channels:	13
Speed:		6
Tempo:		131
Orders:		58 [0 1 4 5 6 7 8 9 16 17 2 3 12 11 10 14 13 14 18 19 20 21 22 21 24 25 24 25 30 31 32 33 34 33 35 36 35 36 37 38 37 38 39 40 41 42 39 40 41 42 46 47 48 49 50 43 44 45]
Pan:		[56 40 80 80 40 56 96 24 96 56 96 24 56 64 64 64 64 64 64 64 64 64 64 64 64 64 64 64 64 64 64 64]
Raw:		{Pad:26 Filetype:16 _:0 Length:60 NumInstruments:41 NumPatterns:51 Flags:0 Tracker:4896 SampleFormat:2 _:[0 0 0 0] GlobalVolume:64 Speed:6 Tempo:131 MasterVolume:176 _:0 Panning:252 _:[0 0 0 0 0 0 0 0] _:[0 0] ChannelSettings:[0 8 1 9 2 10 3 11 4 12 5 13 6 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255 255]}
Instrument 0 x00
	Name:		kohwq
	Length:		37112
[...]
Pattern 0 (x00)
00: E-407..... C-50801... .....10... D#50801... .....13... .......... .......... .......... .......... .......... A-40901... .......... .......... 
01: .......... .....03... .....10... .....03... .....12... .......... .......... .......... .......... .......... .....01... .......... .......... 
02: .......... .....06... .....0F... .....06... .....11... .......... .......... .......... .......... .......... .....01... .......... .......... 
03: .......... .....09... .....0F... .....09... .....10... .......... .......... .......... .......... .......... .....02... .......... .......... 
04: .......... .....0B... .....0E... .....0B... .....10... .......... .......... .......... .......... .......... .....02... .......... .......... 
[...]
```

# Development

You will need Go 1.21 or later. You will also need to create a `go.work` file:

```bash
go work init
go work use .
go work use ./cmd/mod{play,wav,dump}
```

# Testing

Testing a music player is a little tricky because the output is audio data. There are unit tests for the note trigger logic, integrations tests which compare the player against golden output, and manual listening tests where I compare output to other players.

### Unit tests

Currently there are unit tests for the note triggering logic in the player. I determined the logic by experimenting in ScreamTracker 3 and then codifying the behavior in unit tests. Over time I will increase the coverage of unit tests.

```bash
$ go test .
```

### Integration tests

There are two scripts `make_golden.sh` and `check_against_golden.sh`. The first runs `modwav` for each of the included songs to produce "golden" WAVE files. The second script re-runs `modwav` to a temporary directory and compares the output to the corresponding golden file. The comparison uses the `cmp` utility, so it's a trivial byte for byte comparison. These scripts are really only useful during refactors to verify that the output has not changed. Almost any other change affects the output so these tests will fail.

# MOD and S3M files

You can find tracker files at [The Mod Archive](https://modarchive.org/) but I included a small selection in the `mods` folder that are used to test playback:

`space_debris.mod` - one of the most popular MODs on The Mod Archive\
`dope.mod` - From the PC demo [DOPE](http://www.pouet.net/prod.php?which=37) by Complex\
`believe.mod` - From the 64kb PC intro [Believe](http://www.pouet.net/prod.php?which=1151) by Valhalla\
`caero.s3m` - From the PC demo [Caero](https://www.pouet.net/prod.php?which=2163) by Plant & Electromotive Force

# Technical Docs

FireLight's [MOD](docs/fmoddoc.txt) and [S3M](docs/fs3mdoc.txt) format documents were the most useful documents. I converted the box drawing characters from PC code page 437 in the original docs into Unicode. The official ScreamTracker 3 [TECH.DOC](docs/s3m_tech.doc) was also handy.

I used [micromod](https://github.com/martincameron/micromod), [MilkyTracker](https://github.com/milkytracker/MilkyTracker) and [libxmp](https://github.com/libxmp/libxmp) as implementation guides.

# Compiling PortAudio

(These notes mainly for myself, `$MODPLAYER` and `$PORTAUDIO` are the directory of this repo and portaudio respectively)

```
git clone https://github.com/PortAudio/portaudio $PORTAUDIO
cd $PORTAUDIO
./configure
# This will generate the static and dynamic library files that are needed
make

# Mac OSX installation instructions (is there a better way?)
sudo mkdir /usr/local/lib  # Gross!
sudo cp $PORTAUDIO/lib/.libs/libportaudio.2.dylib /usr/local/lib
```

# TODO

- Finish S3M support
- Increase unit test coverage
- Add sample interpolation into mixer for improved sound quality
