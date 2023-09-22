package main

import (
	"encoding/binary"
	"io"
	"log"
	"net"

	proto "github.com/Nekhaevalex/fwsprotocol"
	"github.com/Nekhaevalex/queue"
)

// Object that handles connection between app and it's windows on screen
type AppHandler struct {
	pid      pid                     // Unix pid of app
	screen   *Screen                 // screen pointer
	conn     net.Conn                // Unix domain socket descriptor
	reply    chan proto.Request      // reply outcome channel (to app)
	event    chan proto.EventRequest // events outcome channel (to app)
	app_quit chan int                // app only quit
	windows  []proto.ID              // Array with all app's windows IDs
	quit     chan int                // global quit message channel
}

func (handler *AppHandler) handleHelper(replies *queue.ChannelQueue[proto.Request], events *queue.ChannelQueue[proto.EventRequest]) {
	for {
		select {
		case <-handler.app_quit:
			return
		case <-handler.quit:
			return
		case new_reply := <-replies.Dequeue():
			switch creat_reply := new_reply.(type) {
			case *proto.ReplyCreationRequest:
				handler.windows = append(handler.windows, creat_reply.Id)
			}
			_, err := handler.conn.Write(new_reply.Encode())
			if err != nil {
				log.Fatal(err)
			}
		case new_event := <-events.Dequeue():
			_, err := handler.conn.Write(new_event.Encode())
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

// AppHandler main function. Must be started via go operator
func (handler *AppHandler) handle() {
	defer handler.conn.Close()
	// register app
	handler.event, handler.reply, handler.quit = handler.screen.registerApp(handler.pid)
	handler.windows = make([]proto.ID, 0)
	// start request reciever
	go handler.requestReciever()

	// prepare queues
	replies := queue.NewChannelQueue[proto.Request](1024)
	events := queue.NewChannelQueue[proto.EventRequest](1024)
	// launch helper
	go handler.handleHelper(replies, events)

	// notify app that handler is set up and ready
	handler.notifyApp()
	// handle events & replies
	for {
		select {
		// handle reply
		case new_reply := <-handler.reply:
			replies.Enqueue(new_reply)
		// handle new event
		case new_event := <-handler.event:
			events.Enqueue(new_event)
		// handle quits
		case <-handler.app_quit:
			return
		case <-handler.quit:
			return
		}
	}
}

// System service that listens to incomming requests from app
func (handler *AppHandler) requestReciever() {
	for {
		buff := make([]byte, 0, 1024)
		partial_buff := make([]byte, 512)
		total_len := 0
		for {
			n, err := handler.conn.Read(partial_buff)
			// Check error
			if err != nil {
				switch err {
				case io.EOF:
					handler.closeAllWindows()
					handler.app_quit <- 1
					return
				default:
					log.Fatal(err)
				}
			}
			buff = append(buff, partial_buff[:n]...)
			total_len += n
			if n < cap(partial_buff) {
				break
			}
		}
		select {
		case <-handler.quit:
			return
		default:
			msg := proto.Msg(buff[:total_len])
			handler.screen.enqueueRequest(msg)
			handler.acknowledge(msg)
		}

	}
}

// Notify app about transactions ready
func (handler *AppHandler) notifyApp() {
	msg := []byte("READY")
	_, err := handler.conn.Write(msg)
	if err != nil {
		log.Fatal(err)
	}
}

func (handler *AppHandler) acknowledge(msg proto.Msg) {
	header := proto.Header(msg[0])
	switch header {
	case proto.DRAW, proto.DRAW_FILL, proto.RENDER, proto.RESIZE, proto.MOVE, proto.FOCUS, proto.UNFOCUS, proto.DELETE:
		payload := []uint8(msg)[1:]
		id := proto.ID(binary.LittleEndian.Uint32(payload[0:4]))
		ack := proto.AckRequest{Id: id}
		handler.conn.Write(ack.Encode())
	default:
		return
	}
}

func (handler *AppHandler) closeAllWindows() {
	for _, id := range handler.windows {
		deleter := proto.DeleteRequest{Id: id}
		handler.screen.enqueueRequest(deleter.Encode())
	}
}
