package main

import (
	"encoding/binary"
	"log"
	"net"
	"os"

	proto "github.com/Nekhaevalex/fwsprotocol"

	"github.com/nsf/termbox-go"
)

// Shortcut for process id
type pid int

func main() {
	defer func() {
		termbox.SetInputMode(termbox.InputEsc)
		termbox.Close()
	}()

	initTermbox()
	width, height := termbox.Size()
	screen := NewScreen(0, width, height, termbox.OutputRGB)
	termbox.SetOutputMode(screen.outputMode)
	// Initializing termbox canvas
	// Setting up screen handler
	go screen.handle()

	// Establish socket for incomming connections
	// Connection accept loop
	// get pid
	// Launch app handler
	accepter(screen)
}

func accepter(screen *Screen) {
	if err := os.RemoveAll(proto.FWS_SOCKET); err != nil {
		log.Fatal("socket path error", err)
	}
	l, err := net.Listen("unix", proto.FWS_SOCKET)
	if err != nil {
		log.Fatal("listen error:", err)
	}
	defer l.Close()

	for {
		select {
		case <-screen.quit:
			return
		default:
			// Accept connection
			conn, err := l.Accept()
			if err != nil {
				log.Fatal("accept error:", err)
			}

			// Receive PID
			pid_cache := make([]byte, 64)
			n, err := conn.Read(pid_cache)
			if err != nil {
				log.Fatal("pid request error:", err)
			}
			pid := pid(binary.LittleEndian.Uint32(pid_cache[:n]))

			// Create new handler
			app := AppHandler{pid: pid, screen: screen, conn: conn}
			go app.handle()
		}
	}
}

func initTermbox() {
	err := termbox.Init()
	if err != nil {
		log.Fatal("termbox initialization error", err)
	}
	termbox.SetOutputMode(termbox.OutputRGB)
	termbox.SetInputMode(termbox.InputEsc | termbox.InputMouse)
}
