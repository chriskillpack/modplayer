# MOD and S3M player

_Work in progress_

A Go package to play [MOD](<https://en.wikipedia.org/wiki/MOD_(file_format)>) and [S3M](https://en.wikipedia.org/wiki/S3M) files.

Back in the mid-90's I was active in the PC demoscene and always relied on other people's player code for the music. I never knew how they worked, so I decided many many years later to sit down and code one.

Right now only MOD and S3M files are supported, with S3M support being very incomplete.

# Usage

The package consists of two main parts, a `Song` and a `Player`. The `Song` struct represents a parsed MOD or S3M file, use `NewMODSongFromBytes` or `NewS3MSongFromBytes` to parse a byte slice holding a music file into a `Song`. Use the correct function for the file type. With a `Song` you can create a `Player` instance. Then call `GenerateAudio` on the `Player` instance to generate raw audio output which you send to an audio device (the `modplay` command) or serialize to disk (the `modwav` command).

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

# MOD and S3M files

You can find tracker files at [The Mod Archive](https://modarchive.org/) but I included a small selection in the `mods` folder that are used to test playback:

`space_debris.mod` - one of the most popular MODs on The Mod Archive\
`dope.mod` - From the PC demo [DOPE](http://www.pouet.net/prod.php?which=37) by Complex\
`believe.mod` - From the 64kb PC intro [Believe](http://www.pouet.net/prod.php?which=1151) by Valhalla
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
- Add clamping to mixer to prevent clipping
- Add sample interpolation into mixer for improved sound quality
