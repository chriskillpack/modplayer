# MOD player

_Work in progress_

A Go package to play [MOD](<https://en.wikipedia.org/wiki/MOD_(file_format)>) files.

Back in the mid-90's I was active in the PC demoscene and always relied on other people's MOD player code for the music. I never knew how they worked, so I decided many many years later to sit down and code one.

Right now only MOD files are supported. I hope to add support for S3M files (ScreamTracker 3) in the future.

# Usage

The package consists of two main parts, a `Song` and a `Player`. The `Song` struct represents a parsed MOD file, use `NewSongFromBytes` to parse a byte slice holding a MOD file into a `Song`. With a `Song` you can create a `Player` instance. Then call `GenerateAudio` on the `Player` instance to generate raw audio output which you send to an audio device (the `modplay` command) or serialize to disk (the `modwav` command).

# Build

There are two binaries provided, `modwav` which converts MOD files to RIFF WAVE format files. Pure Go code with no third party dependencies.

```bash
cd cmd/modwav
go run . -hz 22050 awesome.mod  # Generate a 22.5Khz WAVE file from awesome.mod called awesome.wav
```

You can use the `-hz` and `-wav` command line options to affect quality (default 44.1Khz) and output file, by default the same filename with a `.wav` extension in the current directory. The `-boost` flag can be used to boost the output volume, but this can cause clipping.

The second binary is `modplay` which uses `portaudio` to play the MOD file to audio out on your computer. I've included the Windows DLL `portaudio_x64.dll`, you will need to compile portaudio for other platforms. Good luck with that, it can be a bit of a hassle.

```bash
cd $MOD_PLAYER/cmd/modplay
PKG_CONFIG_PATH=$PORTAUDIO CGO_CFLAGS="-I $PORTAUDIO/include" CGO_LDFLAGS="-L $PORTAUDIO/lib/.libs" go build .
modplay awesome.mod
```

![Screenshot of modplay](/docs/modplay.png)

# MOD files

You can find MOD files at [The Mod Archive](https://modarchive.org/) but I included a couple in the `mods` folder that I used to test playback:

`space_debris.mod` - one of the most popular MODs on The Mod Archive\
`dope.mod` - From the PC demo [DOPE](http://www.pouet.net/prod.php?which=37) by Complex\
`believe.mod` - From the 64kb PC intro [Believe](http://www.pouet.net/prod.php?which=1151) by Valhalla

# Technical Docs

[FireLight's MOD format document](docs/fmoddoc.txt) was the most useful document. I converted the original doc that is written with box drawing characters from PC code page 437 into Unicode.

I used [micromod](https://github.com/martincameron/micromod) for some implementation ideas.

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

- S3M support
- Fix clicking on looping samples (e.g. in Dope)
