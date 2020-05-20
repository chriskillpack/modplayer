# MOD player

_Work in progress_

A Go package to play [MOD](<https://en.wikipedia.org/wiki/MOD_(file_format)>) files.

Back in the mid-90's I was active in the PC demoscene and always relied on other people's MOD player code for the music. I never knew how they worked, so I decided many many years later to sit down and code one.

Right now only MOD files are supported. I hope to add support for S3M files (ScreamTracker 3) in the future.

# Usage

The package consists of two main parts, a `Song` and a `Player`. The `Song` struct represents a parsed MOD file, use `NewSongFromBytes` to parse a byte slice holding a MOD file into a `Song`. With a `Song` you can create a `Player` instance. Then call `GenerateAudio` on the `Player` instance to generate raw audio output which you send to an audio device (the `modplay` command) or serialize to disk (the `modwav` command).

# Build

There are two binaries provided, `modwav` which converts MOD files to RIFF WAVE format files. It outputs 44.1Khz WAV files (not adjustable for now). Works on all platforms.

```bash
go install ./cmd/modwav
modwav -wav out.wav awesome.mod
```

The second binary is `modplay` which uses `portaudio` to play the MOD file to audio out on your computer. I've included the Windows DLL `portaudio_x64.dll`, you will need to compile portaudio for other platforms. Good luck with that, it can be a bit of a hassle.

```bash
go install ./cmd/modplay
modplay awesome.mod
```

# MOD files

You can find MOD files at [The Mod Archive](https://modarchive.org/) but I included a couple in the `mods` folder that I used to test playback:

`space_debris.mod` - one of the most popular MODs on The Mod Archive\
`dope.mod` - From the PC demo [DOPE](http://www.pouet.net/prod.php?which=37) by Complex\
`believe.mod` - From the 64kb PC intro [Believe](http://www.pouet.net/prod.php?which=1151) by Valhalla

# Docs

[FireLight's MOD format document](docs/fmoddoc.txt) was the most useful document. I converted the original doc that is written with box drawing characters from PC code page 437 into Unicode.

I used [micromod](https://github.com/martincameron/micromod) for some implementation ideas.
