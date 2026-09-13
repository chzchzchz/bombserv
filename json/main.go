package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/chzchzchz/bombserv/server"
)

func main() {
	mb := flag.Int("mb", 1, "size in megabytes")
	out := flag.String("o", "", "output file (default stdout)")
	flag.Parse()

	var f *os.File
	if *out != "" {
		var err error
		f, err = os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
	} else {
		f = os.Stdout
	}

	if err := server.GenerateJSON(f, *mb); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
