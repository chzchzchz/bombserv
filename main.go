package main

import (
	"flag"
	"log/slog"
	"net"
	"os"

	"github.com/chzchzchz/bombserv/server"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	laddrFlag := flag.String("l", ":8080", "listen address")
	paddrFlag := flag.String("P", "corn.cash:8080", "publish address")
	flag.Parse()

	ln, err := net.Listen("tcp", *laddrFlag)
	if err != nil {
		panic(err)
	}
	payloads := server.MakePayloads()
	svc := server.NewServer(payloads)
	if err := svc.Serve(ln, *paddrFlag); err != nil {
		panic(err)
	}
}
