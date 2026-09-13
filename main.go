package main

import (
	"flag"
	"net"

	"github.com/chzchzchz/bombserv/server"
)

func main() {
	server.MakePayloads()

	laddrFlag := flag.String("l", ":8080", "listen address")
	paddrFlag := flag.String("P", "corn.cash:8080", "publish address")
	flag.Parse()

	ln, err := net.Listen("tcp", *laddrFlag)
	if err != nil {
		panic(err)
	}
	if err := server.Serve(ln, *paddrFlag); err != nil {
		panic(err)
	}
}
