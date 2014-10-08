package config

import (
	"log"
	"github.com/mediocregopher/flagconfig"
)

var (
	Src string
	Dst string
	ForceDownload bool
	NumDownloaders int
	ExcludeFiles map[string]struct{}
)

func init() {
	fc := flagconfig.New("growler")
	fc.DisallowConfig();
	fc.SetDelimiter("-")

	fc.RequiredStrParam("src", "The source url of the files to be mirrored")
	fc.RequiredStrParam("dst", "The destination directory to mirror to")
	fc.FlagParam("force-download", "Don't ignore files based on download times, always re-download", false)
	fc.IntParam("num-downloaders", "Number of go-routines to be downloading at any given time", 10)
	fc.StrParams("exclude-file", "Exclude a filename from being stored to disk.  Can be specified multiple times")

	if err := fc.Parse(); err != nil {
		log.Fatal(err)
	}

	Src = fc.GetStr("src")
	if Src[len(Src)-1] != '/' {
		Src += "/"
	}
	Dst = fc.GetStr("dst")
	ForceDownload = fc.GetFlag("force-download")
	NumDownloaders = fc.GetInt("num-downloaders")
	
	ex := fc.GetStrs("exclude-file")
	ExcludeFiles = map[string]struct{}{}
	for _, filename := range ex {
		ExcludeFiles[filename] = struct{}{}
	}
}
