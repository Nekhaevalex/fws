package main

import (
	"container/list"
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"sync"

	proto "github.com/Nekhaevalex/fwsprotocol"

	"github.com/nsf/termbox-go"
)

type pid int

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

// AppHandler main function. Must be started via go operator
func (handler *AppHandler) handle() {
	defer handler.conn.Close()
	// register app
	handler.event, handler.reply, handler.quit = handler.screen.registerApp(handler.pid)
	handler.windows = make([]proto.ID, 0)
	// start request reciever
	go handler.request_reciever()

	// notify app that handler is set up and ready
	handler.notify_app()
	// handle events & replies
	for {
		select {
		// handle reply
		case new_reply := <-handler.reply:
			switch creat_reply := new_reply.(type) {
			case *proto.ReplyCreationRequest:
				handler.windows = append(handler.windows, creat_reply.Id)
			}
			_, err := handler.conn.Write(new_reply.Encode())
			if err != nil {
				log.Fatal(err)
			}
		// handle new event
		case new_event := <-handler.event:
			_, err := handler.conn.Write(new_event.Encode())
			if err != nil {
				log.Fatal(err)
			}
			// log.Printf("%d bytes sent to %d", n, handler.pid)
		// handle quits
		case <-handler.app_quit:
			// log.Printf("[%d] handler quitting", handler.pid)
			return
		case <-handler.quit:
			return
		}
	}
}

// System service that listens to incomming requests from app
func (handler *AppHandler) request_reciever() {
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
					handler.close_all_windows()
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
			handler.screen.EnqueueRequest(msg)
			handler.acknowledge(msg)
		}

	}
}

// Notify app about transactions ready
func (handler *AppHandler) notify_app() {
	msg := []byte("READY")
	_, err := handler.conn.Write(msg)
	if err != nil {
		log.Fatal(err)
	}
	// log.Printf("%d ready notified (%d)\n", handler.pid, n)
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

func (handler *AppHandler) close_all_windows() {
	for _, id := range handler.windows {
		deleter := proto.DeleteRequest{Id: id}
		handler.screen.EnqueueRequest(deleter.Encode())
	}
}

// Object descripting layer on screen
type Layer struct {
	owner         pid
	id            proto.ID
	x, y          int
	width, height int
	render        bool
	canvas        [][]proto.Cell
}

// Layer constructor from issued layer ID and creation request
func newLayer(lID proto.ID, request proto.NewWindowRequest) *Layer {
	layer := new(Layer)
	layer.owner = pid(request.Pid)
	layer.id = lID
	layer.x = request.X
	layer.y = request.Y
	layer.render = false
	layer.make_canvas(request.Width, request.Height)
	return layer
}

// Method allocating canvas
func (layer *Layer) make_canvas(width int, height int) {
	layer.width = width
	layer.height = height
	layer.canvas = make([][]proto.Cell, width)
	for i := range layer.canvas {
		layer.canvas[i] = make([]proto.Cell, height)
	}
}

// Structure providing layer management
type LayerOrder struct {
	layers *list.List
	index  map[proto.ID]*list.Element
}

func (queue *LayerOrder) init() {
	queue.layers = list.New()
	queue.index = make(map[proto.ID]*list.Element)
}

func (queue *LayerOrder) push(layer *Layer) {
	if queue.layers != nil {
		lid := layer.id
		new_element := queue.layers.PushFront(layer)
		queue.index[lid] = new_element
	}
}

func (queue *LayerOrder) get(id proto.ID) *Layer {
	return queue.index[id].Value.(*Layer)
}

func (queue *LayerOrder) top(id proto.ID) {
	queue.layers.MoveToFront(queue.index[id])
}

type prevMouseEvent struct {
	key            termbox.Key
	layer          proto.ID
	owner          pid
	layerX, layerY int
}

func (ev prevMouseEvent) sameLayer(new termbox.Event) bool {
	prevCont := (ev.key == termbox.MouseLeft) || (ev.key == termbox.MouseMiddle) || (ev.key == termbox.MouseRight)
	newCont := (new.Key == termbox.MouseLeft) || (new.Key == termbox.MouseMiddle) || (new.Key == termbox.MouseRight) || (new.Key == termbox.MouseRelease)
	return prevCont && newCont
}

func (ev prevMouseEvent) getPlaneLayerPos(x, y int) (pid, proto.ID, int, int) {
	return ev.owner, ev.layer, (x - ev.layerX), (y - ev.layerY)
}

func (ev *prevMouseEvent) saveState(owner pid, event termbox.Event, d_event proto.EventRequest) {
	ev.key = event.Key
	ev.layer = d_event.Id
	ev.owner = owner
	ev.layerX = event.MouseX - d_event.MouseX
	ev.layerY = event.MouseY - d_event.MouseY
}

// Screen abstraction handler.
type Screen struct {
	id            int                             // screen ID
	width, height int                             // screen size
	objects       LayerOrder                      // layers on the screen
	selected      proto.ID                        // currently selected layer id
	globalEvents  chan termbox.Event              // global events incomming channel
	replyChannels map[pid]chan proto.Request      // reply channels
	eventChannels map[pid]chan proto.EventRequest // dispatched events channels
	prevMouseKey  prevMouseEvent                  // previous pressed mouse event (for window movements)
	requestQueue  []proto.Msg                     // requests queue
	lock          sync.Mutex                      // queue mutex
	quit          chan int                        // quit message channel
}

// Register app handler
func (screen *Screen) registerApp(pid pid) (
	eventChanel chan proto.EventRequest,
	replyChanel chan proto.Request,
	quit chan int) {
	return screen.registerEventChannel(pid),
		screen.registerReplyChannel(pid),
		screen.quit
}

// Issue new layer ID
func (screen *Screen) issueNewLayerID() proto.ID {
	return proto.ID(screen.objects.layers.Len())
}

// Registers new event channel for events dispatching
func (screen *Screen) registerEventChannel(pid pid) chan proto.EventRequest {
	screen.eventChannels[pid] = make(chan proto.EventRequest)
	return screen.eventChannels[pid]
}

// Registers new reply channel for events dispatching
func (screen *Screen) registerReplyChannel(pid pid) chan proto.Request {
	screen.replyChannels[pid] = make(chan proto.Request)
	return screen.replyChannels[pid]
}

func (screen *Screen) render() {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	// log.Printf("Rendering!")
	for elem := screen.objects.layers.Back(); elem != nil; elem = elem.Prev() {
		layer := elem.Value.(*Layer)
		// log.Printf("*Layer found")
		if layer.render {
			// log.Printf("Renderable")
			for lx := 0; lx < layer.width; lx++ {
				for ly := 0; ly < layer.height; ly++ {
					rx := lx + layer.x
					ry := ly + layer.y

					if rx < 0 || rx >= screen.width {
						continue
					}

					if ry < 0 || ry >= screen.height {
						continue
					}

					tbCell := termbox.GetCell(rx, ry)
					pCell := proto.FromTermboxCell(tbCell)
					over := layer.canvas[lx][ly]
					new := over.Over(pCell)
					tbNew := new.ToTerboxCell()
					termbox.SetCell(rx, ry, tbNew.Ch, tbNew.Fg, tbNew.Bg)
				}
			}
		}
	}
	termbox.Flush()
}

func (scren *Screen) EnqueueRequest(request proto.Msg) {
	scren.lock.Lock()
	scren.requestQueue = append(scren.requestQueue, request)
	scren.lock.Unlock()
}

// Screen handling loop. Start via go operator
func (screen *Screen) handle() {
	// start globalEvents_reciever
	go screen.globalEvents_receiver()
	go screen.render_worker()
	// handle requests & events
	for {
		select {
		case globalEvent := <-screen.globalEvents:
			pid, dispatched_event := screen.dispatch_event(globalEvent)
			switch pid {
			case -1:
				// Empty event
			case 0:
				// Send to all apps
				for pid := range screen.eventChannels {
					screen.eventChannels[pid] <- dispatched_event
				}
			default:
				if dispatched_event.Id != screen.selected {
					focus := proto.FocusRequest{Id: dispatched_event.Id}
					screen.EnqueueRequest(focus.Encode())
				}
				screen.eventChannels[pid] <- dispatched_event
			}
		case <-screen.quit:
			return
		}
	}
}

func (screen *Screen) render_worker() {
	for {
		screen.lock.Lock()
		length := len(screen.requestQueue)
		screen.lock.Unlock()
		if length > 0 {
			screen.lock.Lock()
			request := screen.requestQueue[0]
			screen.requestQueue = screen.requestQueue[1:]
			screen.lock.Unlock()
			screen.proceedRequest(request)
		}
	}
}

func (screen *Screen) proceedRequest(request proto.Msg) {
	switch decoded := request.Decode().(type) {
	case *proto.NewWindowRequest:
		screen.implementNewWindow(decoded)
	case *proto.GetRequest:
		screen.implementGet(decoded)
	case *proto.DrawRequest:
		screen.implementDraw(decoded)
	case *proto.DrawFillRequest:
		screen.implementDrawFill(decoded)
	case *proto.RenderRequest:
		screen.implementRender(decoded)
	case *proto.DeleteRequest:
		screen.implementDeleteRequest(decoded)
	case *proto.ResizeRequest:
		screen.implementResize(decoded)
	case *proto.MoveRequest:
		screen.implementMove(decoded)
	case *proto.FocusRequest:
		screen.implementFocus(decoded)
	}
}

func (screen *Screen) implementNewWindow(decoded *proto.NewWindowRequest) {
	lId := screen.issueNewLayerID()
	layer := newLayer(lId, *decoded)
	screen.objects.push(layer)
	reply := &proto.ReplyCreationRequest{Id: lId}
	screen.replyChannels[pid(decoded.Pid)] <- reply
	screen.selected = lId
}

func (screen *Screen) implementGet(decoded *proto.GetRequest) {
	lid := decoded.Id
	x := decoded.X
	y := decoded.Y
	cell := screen.objects.get(lid).canvas[x][y]
	reply := &proto.ReplyGetRequest{C: cell}
	pid := screen.objects.get(lid).owner
	screen.replyChannels[pid] <- reply
}

func (screen *Screen) implementRender(decoded *proto.RenderRequest) {
	lid := decoded.Id
	screen.objects.get(lid).render = true
	screen.render()
}

func (screen *Screen) implementDraw(decoded *proto.DrawRequest) {
	lid := decoded.Id
	x := decoded.X
	y := decoded.Y
	cell := decoded.Cell
	screen.objects.get(lid).render = false
	old_cell := screen.objects.get(lid).canvas[x][y]
	new_cell := cell.Over(old_cell)
	screen.objects.get(lid).canvas[x][y] = new_cell
}

func (screen *Screen) implementFocus(decoded *proto.FocusRequest) {
	lid := decoded.Id
	screen.objects.top(lid)
	screen.selected = lid
	screen.render()
}

func (screen *Screen) implementMove(decoded *proto.MoveRequest) {
	lid := decoded.Id
	x := decoded.X
	y := decoded.Y
	screen.objects.get(lid).render = false
	screen.objects.get(lid).x += x
	screen.objects.get(lid).y += y
}

func (screen *Screen) implementResize(decoded *proto.ResizeRequest) {
	lid := decoded.Id
	width := decoded.Width
	height := decoded.Height
	screen.objects.get(lid).render = false
	screen.objects.get(lid).make_canvas(width, height)
}

func (screen *Screen) implementDeleteRequest(decoded *proto.DeleteRequest) {
	lid := decoded.Id
	for e := screen.objects.layers.Front(); e != nil; e = e.Next() {
		switch lr := e.Value.(type) {
		case *Layer:
			if lr.id == lid {
				screen.objects.layers.Remove(e)
				delete(screen.objects.index, lid)
				next := e.Next()
				if next != nil {
					screen.selected = next.Value.(*Layer).id
				}
			}
		}
	}
	screen.render()
}

func (screen *Screen) implementDrawFill(decoded *proto.DrawFillRequest) {
	lid := decoded.Id
	w := decoded.Width
	h := decoded.Height
	img := decoded.Img
	screen.objects.get(lid).render = false
	for i := 0; i < w; i++ {
		for j := 0; j < h; j++ {
			screen.objects.get(lid).canvas[i][j].Ch = img[i][j].Ch
			screen.objects.get(lid).canvas[i][j].Fg = img[i][j].Fg
			screen.objects.get(lid).canvas[i][j].Bg = img[i][j].Bg
			screen.objects.get(lid).canvas[i][j].Attribute = img[i][j].Attribute
		}
	}
}

func (screen *Screen) globalEvents_receiver() {
	// log.Printf("Launching globalEvents receiver")
	for {
		event := termbox.PollEvent()
		select {
		case screen.globalEvents <- event:
		case <-screen.quit:
			// log.Printf("[globalEvents_receiver] Quit signal received")
			return
		}
	}
}

func (screen *Screen) dispatch_event(event termbox.Event) (pid, proto.EventRequest) {
	// log.Printf("Attempting to dispatch event\n")
	switch event.Type {
	case termbox.EventResize:
		newWidth, newHeight := termbox.Size()
		screen.height = newHeight
		screen.width = newWidth
		// Send resize to all apps
		return 0, proto.EventRequest{Id: screen.selected, Event: event}
	case termbox.EventKey:
		if screen.selected != 0 {
			// log.Printf("Sending to %d...", screen.selected)
			return screen.objects.get(screen.selected).owner, proto.EventRequest{Id: screen.selected, Event: event}
		}
		return -1, proto.EventRequest{}
	case termbox.EventMouse:
		// Search sender on screen
		layers := screen.objects.layers
		x, y := event.MouseX, event.MouseY
		look_up_area := func() (pid, proto.ID, int, int) {
			if screen.prevMouseKey.sameLayer(event) {
				return screen.prevMouseKey.getPlaneLayerPos(x, y)
			}
			for e := layers.Front(); e != nil; e = e.Next() {
				switch layer := e.Value.(type) {
				case *Layer:
					if x >= layer.x && x <= (layer.x+layer.width-1) && y >= layer.y && y <= (layer.y+layer.height-1) {
						return layer.owner, layer.id, (x - layer.x), (y - layer.y)
					}
				}
			}
			return -1, 0, 0, 0
		}
		owner, lid, newX, newY := look_up_area()

		// if screen.prevMouseKey.Key == termbox.MouseLeft {
		// 	// EXPERIMENTAL!!!
		// 	screen.prevMouseKey = event
		// }

		dispatched := event
		dispatched.MouseX = newX
		dispatched.MouseY = newY
		request := proto.EventRequest{Id: lid, Event: dispatched}
		screen.prevMouseKey.saveState(owner, event, request)
		return owner, request
	case termbox.EventInterrupt:
		// log.Printf("Interrupt event!")
		return 0, proto.EventRequest{Id: screen.selected, Event: event}
	case termbox.EventNone:
		// log.Printf("None event")
		return -1, proto.EventRequest{Id: screen.selected, Event: event}
	default:
		return -1, proto.EventRequest{}
	}
}

func screen_init(id, width, height int) *Screen {
	screen := &Screen{id: 0, width: width, height: height}
	screen.objects = LayerOrder{}
	screen.objects.init()
	screen.globalEvents = make(chan termbox.Event)
	screen.quit = make(chan int)
	screen.replyChannels = make(map[pid]chan proto.Request)
	screen.eventChannels = make(map[pid]chan proto.EventRequest)
	screen.selected = 0
	screen.requestQueue = make([]proto.Msg, 0, 100)
	return screen
}

func main() {
	defer func() {
		termbox.SetInputMode(termbox.InputEsc)
		termbox.Close()
	}()
	// Initializing termbox canvas
	// Setting up screen handler
	init_termbox()

	width, height := termbox.Size()
	screen := screen_init(0, width, height)
	go screen.handle()

	// Establish socket for incomming connections
	// Connection accept loop
	// get pid
	// Launch app handler
	accepter(screen)
}

func accepter(screen *Screen) {
	if err := os.RemoveAll(proto.FWS_SOCKET); err != nil {
		log.Fatal(err)
	}
	// log.Printf("Listening to socket at %s\n", proto.FWS_SOCKET)
	l, err := net.Listen("unix", proto.FWS_SOCKET)
	if err != nil {
		log.Fatal("listen error:", err)
	}
	defer l.Close()

	for {
		select {
		case <-screen.quit:
			// log.Printf("Server shut down recieved, quitting.")
			return
		default:
			// Accept connection
			conn, err := l.Accept()
			if err != nil {
				log.Fatal("accept error:", err)
			}
			// log.Printf("Connection accepted\n")

			// Receive PID
			pid_cache := make([]byte, 64)
			n, err := conn.Read(pid_cache)
			if err != nil {
				log.Fatal("pid request error:", err)
			}
			pid := pid(binary.LittleEndian.Uint32(pid_cache[:n]))
			// log.Printf("Process %d connected", pid)

			// Create new handler
			app := AppHandler{pid: pid, screen: screen, conn: conn}
			go app.handle()
		}
	}
}

func init_termbox() {
	err := termbox.Init()
	if err != nil {
		log.Fatal(err)
	}
	termbox.SetOutputMode(termbox.OutputRGB)
	termbox.SetInputMode(termbox.InputEsc | termbox.InputMouse)
}
