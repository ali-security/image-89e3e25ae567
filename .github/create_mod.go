// Command create_mod writes a Go module zip for a directory of module source,
// using the same golang.org/x/mod/zip encoder the module proxy uses.
package main

import (
	"log"
	"os"

	"golang.org/x/mod/module"
	"golang.org/x/mod/zip"
)

func main() {
	if len(os.Args) != 5 {
		log.Fatal("usage: create_mod <module-path> <version> <dir> <out.zip>")
	}
	mv := module.Version{Path: os.Args[1], Version: os.Args[2]}
	f, err := os.Create(os.Args[4])
	if err != nil {
		log.Fatal(err)
	}
	if err := zip.CreateFromDir(f, mv, os.Args[3]); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
