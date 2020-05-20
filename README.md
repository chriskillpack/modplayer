# MOD player

Work in progress

Play [MOD](<https://en.wikipedia.org/wiki/MOD_(file_format)>) files.

Back in the mid-90's I was active in the PC demoscene and always relied on other people's MOD player code for the music. I never knew how they worked, so I decided many many years later to sit down and code one.

Right now only MOD files are supported. I hope to add S3M support.

# Build

There are two binaries provided, `modwav` which converts MOD files to RIFF WAVE format files. This works on all platforms.

```bash
go install ./cmd/modwav
modwav -wav out.wav awesome.mod
```

The second binary is `modplay` which uses `portaudio` to play the MOD file. Only Windows supported for now. You will need to make sure `portaudio_x64.dll` is part of your DLL search path.

```bash
go install ./cmd/modplay
modplay awesome.mod
```

# MOD files

You can find MOD files at [The Mod Archive](https://modarchive.org/) but I included a couple in the `mods` folder that I used to test playback:

space_debris.mod - one of the most popular MODs on The Mod Archive
dope.mod - From the PC demo [DOPE](http://www.pouet.net/prod.php?which=37) by Complex
believe.mod - From the 64kb intro [Believe](http://www.pouet.net/prod.php?which=1151) by Valhalla

# Docs

[FireLight's MOD format document](docs/fmoddoc.txt) was the most useful document. I converted the original doc that is written with box drawing characters from PC code page 437 into Unicode.

I used [micromod](https://github.com/martincameron/micromod) for some implementation ideas.
